package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
)

const (
	updateChannelKey        = "update_channel"
	lastUpdateCheckAtKey    = "last_update_check_at"
	lastUpdateVersionKey    = "last_update_version"
	lastUpdateAttemptAtKey  = "last_update_attempt_at"
	lastUpdateTargetKey     = "last_update_target"
	lastUpdateResultKey     = "last_update_result"
	lastUpdateErrorKey      = "last_update_error"
	defaultUpdateChannel    = "stable"
	defaultSharedDataMount  = "/app/data"
	updateRunnerCommandArg  = "updater-runner"
	updateStateRelativePath = "update/state.json"
)

func UpdateRunnerCommandArg() string {
	return updateRunnerCommandArg
}

type UpdateStatus struct {
	Status                string   `json:"status"`
	Message               string   `json:"message"`
	CurrentVersion        string   `json:"currentVersion"`
	LatestVersion         string   `json:"latestVersion"`
	TargetVersion         string   `json:"targetVersion"`
	Channel               string   `json:"channel"`
	HasUpdate             bool     `json:"hasUpdate"`
	CurrentImage          string   `json:"currentImage,omitempty"`
	TargetImage           string   `json:"targetImage,omitempty"`
	PreviousVersion       string   `json:"previousVersion,omitempty"`
	PreviousImage         string   `json:"previousImage,omitempty"`
	RollbackAvailable     bool     `json:"rollbackAvailable"`
	RollbackTargetVersion string   `json:"rollbackTargetVersion,omitempty"`
	RollbackTargetImage   string   `json:"rollbackTargetImage,omitempty"`
	ReleaseSource         string   `json:"releaseSource,omitempty"`
	ReleaseNotesURL       string   `json:"releaseNotesUrl,omitempty"`
	GitHubReleaseURL      string   `json:"githubReleaseUrl,omitempty"`
	HelperContainer       string   `json:"helperContainer,omitempty"`
	LastCheckedAt         *string  `json:"lastCheckedAt,omitempty"`
	StartedAt             *string  `json:"startedAt,omitempty"`
	CompletedAt           *string  `json:"completedAt,omitempty"`
	Error                 *string  `json:"error,omitempty"`
	Logs                  []string `json:"logs,omitempty"`
	NeedsDockerSocket     bool     `json:"needsDockerSocket"`
	DeploymentMode        string   `json:"deploymentMode"`
	DownloadURL           string   `json:"downloadUrl,omitempty"`
}

type UpdateRelease struct {
	Version          string
	Channel          string
	Image            string
	ReleaseSource    string
	ReleaseNotesURL  string
	GitHubReleaseURL string
	Assets           []gitHubAsset
}

type Updater struct {
	mu         sync.Mutex
	cfg        *config.AppConfig
	pool       *pgxpool.Pool
	httpClient *http.Client
	logBuffer  *LogBuffer
	statePath  string
	status     UpdateStatus
}

type dockerHubTagsResponse struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
	Next *string `json:"next"`
}

type gitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type gitHubRelease struct {
	TagName    string        `json:"tag_name"`
	HTMLURL    string        `json:"html_url"`
	Prerelease bool          `json:"prerelease"`
	Assets     []gitHubAsset `json:"assets"`
}

func NewUpdater(cfg *config.AppConfig, pool *pgxpool.Pool, httpClient *http.Client, logBuffer *LogBuffer) *Updater {
	u := &Updater{
		cfg:        cfg,
		pool:       pool,
		httpClient: httpClient,
		logBuffer:  logBuffer,
		statePath:  filepath.Join(cfg.DataDir, updateStateRelativePath),
		status: UpdateStatus{
			Status:            "idle",
			Message:           "未检查更新",
			CurrentVersion:    cfg.Version,
			Channel:           defaultUpdateChannel,
			NeedsDockerSocket: true,
		},
	}
	u.reloadStateLocked()
	u.finalizeRestartStateLocked()
	return u
}

// finalizeRestartStateLocked 启动时如果 state 处于 "restarting"(上次 apply 后 exec 成功触发了重启),
// 比较本次运行版本与 TargetVersion:
// - 匹配 → 标记 completed,当前版本刷新
// - 不匹配 → 标记 failed,进程管理器把旧进程又拉了起来 / exec 失败 / 用户手动换了二进制
func (u *Updater) finalizeRestartStateLocked() {
	if u.status.Status != "restarting" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.CompletedAt = &now
	if u.status.RollbackTargetVersion != "" {
		if u.cfg.Version == u.status.RollbackTargetVersion {
			u.status.Status = "completed"
			u.status.Message = "回滚完成"
			u.status.CurrentVersion = u.cfg.Version
			u.status.TargetVersion = ""
			u.status.TargetImage = ""
			u.status.PreviousVersion = ""
			u.status.PreviousImage = ""
			u.status.RollbackTargetVersion = ""
			u.status.RollbackTargetImage = ""
			u.status.Error = nil
			u.status.HasUpdate = false
			u.status.RollbackAvailable = false
			u.appendLogLocked(fmt.Sprintf("回滚成功,当前版本 %s", u.cfg.Version))
		} else {
			u.status.Status = "failed"
			u.status.Message = "重启后版本未回滚"
			msg := fmt.Sprintf("expected %s but running %s", u.status.RollbackTargetVersion, u.cfg.Version)
			u.status.Error = &msg
			u.appendLogLocked("回滚失败: " + msg)
		}
		u.persistStateLocked()
		return
	}
	if u.status.TargetVersion != "" && u.cfg.Version == u.status.TargetVersion {
		u.status.Status = "completed"
		u.status.Message = "更新完成"
		u.status.CurrentVersion = u.cfg.Version
		u.status.Error = nil
		u.status.HasUpdate = false
		u.appendLogLocked(fmt.Sprintf("重启成功,当前版本 %s", u.cfg.Version))
	} else {
		u.status.Status = "failed"
		u.status.Message = "重启后版本未更新"
		msg := fmt.Sprintf("expected %s but running %s", u.status.TargetVersion, u.cfg.Version)
		u.status.Error = &msg
		u.appendLogLocked("更新失败: " + msg)
	}
	u.persistStateLocked()
}

func (u *Updater) GetStatus(ctx context.Context) UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	u.status.CurrentVersion = u.cfg.Version
	if channel := u.getConfigValue(ctx, updateChannelKey); channel != "" {
		u.status.Channel = normalizeUpdateChannel(channel)
	}
	u.refreshRollbackAvailabilityLocked()
	u.applyDeploymentLocked()
	return cloneUpdateStatus(u.status)
}

// applyDeploymentLocked 按当前部署模式回填 DeploymentMode / NeedsDockerSocket,
// 以及 manual 模式的 DownloadURL。调用前需持有 u.mu。
func (u *Updater) applyDeploymentLocked() {
	mode := DetectDeploymentMode()
	u.status.DeploymentMode = string(mode)
	u.status.NeedsDockerSocket = mode == DeployDocker && !u.dockerSocketAvailable()
	if mode == DeployManual {
		u.status.DownloadURL = u.buildManualDownloadURL()
	} else {
		u.status.DownloadURL = ""
	}
}

// refreshRollbackAvailabilityLocked 根据当前部署模式和已记录的上一版本信息刷新可回滚标记。
// 调用前需持有 u.mu。
func (u *Updater) refreshRollbackAvailabilityLocked() {
	mode := DetectDeploymentMode()
	switch mode {
	case DeployDocker:
		u.status.RollbackAvailable = strings.TrimSpace(u.status.PreviousImage) != ""
	case DeployBinary:
		backupPath := u.binaryBackupPath(u.status.PreviousVersion)
		if backupPath == "" {
			u.status.RollbackAvailable = false
		} else if _, err := os.Stat(backupPath); err == nil {
			u.status.RollbackAvailable = true
		} else {
			u.status.RollbackAvailable = false
		}
	default:
		u.status.RollbackAvailable = false
	}
	if !u.status.RollbackAvailable {
		u.status.RollbackTargetVersion = ""
		u.status.RollbackTargetImage = ""
	}
}

func (u *Updater) dockerSocketAvailable() bool {
	socketPath := strings.TrimSpace(u.cfg.UpdateDockerSocket)
	if socketPath == "" {
		return false
	}
	if _, err := os.Stat(socketPath); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dockerClient := newDockerClient(socketPath)
	defer dockerClient.CloseIdleConnections()
	return dockerClient.ping(ctx) == nil
}

// buildManualDownloadURL 给 Windows/未支持平台的用户返回 GitHub Release 的直链。
// 如果 Check 已经取到精确 asset 名就用精确链接;否则退回到 release 的 HTML 页面。
func (u *Updater) buildManualDownloadURL() string {
	if u.status.GitHubReleaseURL != "" {
		return u.status.GitHubReleaseURL
	}
	repo := strings.TrimSpace(u.cfg.UpdateGitHubRepo)
	if repo == "" {
		return ""
	}
	channel := normalizeUpdateChannel(u.status.Channel)
	if channel == "nightly" {
		return fmt.Sprintf("https://github.com/%s/releases/tag/nightly", repo)
	}
	return fmt.Sprintf("https://github.com/%s/releases/latest", repo)
}

func (u *Updater) Check(ctx context.Context) (UpdateStatus, error) {
	u.mu.Lock()
	u.reloadStateLocked()
	u.status.Status = "checking"
	u.status.Message = "正在检查更新"
	u.status.Error = nil
	u.status.CompletedAt = nil
	u.status.CurrentVersion = u.cfg.Version
	channel := normalizeUpdateChannel(u.getConfigValue(ctx, updateChannelKey))
	if channel == "" {
		channel = defaultUpdateChannel
	}
	u.status.Channel = channel
	u.persistStateLocked()
	u.mu.Unlock()

	release, err := u.resolveLatestRelease(ctx, channel)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	u.status.CurrentVersion = u.cfg.Version
	u.status.Channel = channel
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.LastCheckedAt = &now
	_ = u.setConfigValue(ctx, lastUpdateCheckAtKey, now)
	if err != nil {
		u.status.Status = "failed"
		u.status.Message = "检查更新失败"
		msg := err.Error()
		u.status.Error = &msg
		_ = u.setConfigValue(ctx, lastUpdateErrorKey, msg)
		u.persistStateLocked()
		return cloneUpdateStatus(u.status), err
	}

	u.status.LatestVersion = release.Version
	u.status.TargetVersion = release.Version
	u.status.TargetImage = release.Image
	u.status.ReleaseSource = release.ReleaseSource
	u.status.ReleaseNotesURL = release.ReleaseNotesURL
	u.status.GitHubReleaseURL = release.GitHubReleaseURL
	u.status.HasUpdate = hasUpdateForChannel(channel, u.cfg.Version, release.Version)
	if u.status.HasUpdate {
		u.status.Status = "available"
		u.status.Message = fmt.Sprintf("发现新版本 %s", release.Version)
	} else {
		u.status.Status = "idle"
		u.status.Message = "当前已是最新版本"
	}
	u.applyDeploymentLocked()
	_ = u.setConfigValue(ctx, lastUpdateVersionKey, release.Version)
	_ = u.setConfigValue(ctx, lastUpdateResultKey, "checked")
	u.persistStateLocked()
	return cloneUpdateStatus(u.status), nil
}

func (u *Updater) SetChannel(ctx context.Context, channel string) (UpdateStatus, error) {
	channel = normalizeUpdateChannel(channel)
	if channel == "" {
		return u.GetStatus(ctx), fmt.Errorf("invalid update channel")
	}
	if err := u.setConfigValue(ctx, updateChannelKey, channel); err != nil {
		return u.GetStatus(ctx), err
	}

	u.mu.Lock()
	u.reloadStateLocked()
	u.status.Channel = channel
	u.status.Message = "更新通道已保存"
	u.persistStateLocked()
	u.mu.Unlock()
	return u.Check(ctx)
}

func (u *Updater) MarkBackingUp() UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "backing_up"
	u.status.Message = "正在创建更新前备份"
	u.status.StartedAt = &now
	u.status.CompletedAt = nil
	u.status.Error = nil
	u.appendLogLocked("开始创建更新前备份")
	u.persistStateLocked()
	return cloneUpdateStatus(u.status)
}

func (u *Updater) StartApply(ctx context.Context) (UpdateStatus, error) {
	mode := DetectDeploymentMode()
	switch mode {
	case DeployDocker:
		return u.startApplyDocker(ctx)
	case DeployBinary:
		return u.startApplyBinary(ctx)
	case DeployManual:
		return u.GetStatus(ctx), fmt.Errorf("platform does not support auto-update, please download manually")
	default:
		return u.GetStatus(ctx), fmt.Errorf("unknown deployment mode")
	}
}

func (u *Updater) startApplyDocker(ctx context.Context) (UpdateStatus, error) {
	u.mu.Lock()
	u.reloadStateLocked()
	if isUpdateTaskActive(u.status.Status) {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("update task already running")
	}
	u.mu.Unlock()

	checked, err := u.Check(ctx)
	if err != nil {
		return checked, err
	}
	if !checked.HasUpdate || checked.TargetImage == "" {
		return checked, fmt.Errorf("no update available")
	}
	if _, err := os.Stat(u.cfg.UpdateDockerSocket); err != nil {
		return checked, fmt.Errorf("docker socket unavailable: %w", err)
	}

	dockerClient := newDockerClient(u.cfg.UpdateDockerSocket)
	defer dockerClient.CloseIdleConnections()

	containerID, err := currentContainerID()
	if err != nil {
		return checked, err
	}
	inspect, err := dockerClient.inspectContainer(ctx, containerID)
	if err != nil {
		return checked, fmt.Errorf("inspect current container: %w", err)
	}

	helperName := fmt.Sprintf("fyms-updater-%d", time.Now().Unix())
	helperBinds := buildHelperBinds(u.cfg.UpdateDockerSocket, inspect, defaultSharedDataMount)
	currentImage := rollbackImageRef(inspect)
	helperEnv := []string{
		"FYMS_UPDATE_RUNNER=1",
		"FYMS_UPDATE_ACTION=apply",
		fmt.Sprintf("FYMS_UPDATE_DOCKER_SOCKET=%s", u.cfg.UpdateDockerSocket),
		fmt.Sprintf("FYMS_UPDATE_TARGET_CONTAINER=%s", containerID),
		fmt.Sprintf("FYMS_UPDATE_TARGET_IMAGE=%s", checked.TargetImage),
		fmt.Sprintf("FYMS_UPDATE_TARGET_VERSION=%s", checked.TargetVersion),
		fmt.Sprintf("FYMS_UPDATE_STATE_PATH=%s", u.statePath),
	}

	helperBody := map[string]any{
		"Image": currentImage,
		"Env":   helperEnv,
		"Cmd":   []string{updateRunnerCommandArg},
		"Labels": map[string]string{
			"fyms.update.helper": "true",
		},
		"HostConfig": map[string]any{
			"AutoRemove":  true,
			"Binds":       helperBinds,
			"NetworkMode": "none",
		},
	}

	helperID, err := dockerClient.createContainer(ctx, helperName, helperBody)
	if err != nil {
		return checked, fmt.Errorf("create update helper: %w", err)
	}
	u.mu.Lock()
	u.reloadStateLocked()
	u.status.CurrentImage = currentImage
	u.status.PreviousVersion = u.cfg.Version
	u.status.PreviousImage = currentImage
	u.status.RollbackAvailable = true
	u.status.RollbackTargetVersion = ""
	u.status.RollbackTargetImage = ""
	u.persistStateLocked()
	u.mu.Unlock()

	if err := dockerClient.startContainer(ctx, helperID); err != nil {
		return checked, fmt.Errorf("start update helper: %w", err)
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "restarting"
	u.status.Message = "更新任务已启动，服务即将重启"
	u.status.HelperContainer = helperName
	u.status.StartedAt = &now
	u.status.CompletedAt = nil
	u.status.Error = nil
	u.status.TargetImage = checked.TargetImage
	u.status.TargetVersion = checked.TargetVersion
	u.status.CurrentVersion = u.cfg.Version
	u.status.CurrentImage = currentImage
	u.status.PreviousVersion = u.cfg.Version
	u.status.PreviousImage = currentImage
	u.status.RollbackAvailable = true
	u.status.RollbackTargetVersion = ""
	u.status.RollbackTargetImage = ""
	u.appendLogLocked(fmt.Sprintf("更新助手已启动: %s", helperName))
	u.persistStateLocked()
	_ = u.setConfigValue(ctx, lastUpdateAttemptAtKey, now)
	_ = u.setConfigValue(ctx, lastUpdateTargetKey, checked.TargetImage)
	_ = u.setConfigValue(ctx, lastUpdateResultKey, "started")
	return cloneUpdateStatus(u.status), nil
}

func (u *Updater) MarkCompleted(version string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "completed"
	u.status.Message = "更新完成"
	u.status.CurrentVersion = version
	u.status.CompletedAt = &now
	u.status.Error = nil
	u.status.HasUpdate = false
	u.appendLogLocked(fmt.Sprintf("更新完成，当前版本: %s", version))
	u.persistStateLocked()
}

func (u *Updater) MarkFailure(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "failed"
	u.status.Message = "更新失败"
	u.status.CompletedAt = &now
	if err != nil {
		msg := err.Error()
		u.status.Error = &msg
		u.appendLogLocked("更新失败: " + msg)
	}
	u.persistStateLocked()
}

func (u *Updater) resolveLatestRelease(ctx context.Context, channel string) (UpdateRelease, error) {
	tags, err := u.fetchDockerTags(ctx)
	if err != nil {
		return UpdateRelease{}, err
	}
	latestTag := selectLatestTag(tags, channel)
	if latestTag == "" {
		return UpdateRelease{}, fmt.Errorf("no matching docker tags found")
	}
	release := UpdateRelease{
		Version:       latestTag,
		Channel:       channel,
		Image:         fmt.Sprintf("%s:%s", u.cfg.UpdateImageRepo, latestTag),
		ReleaseSource: "docker",
	}
	var ghRelease *gitHubRelease
	if channel == "nightly" {
		// nightly 镜像 tag 每次变化,但 GitHub Release 是固定的滚动 pre-release,tag 恒为 "nightly"。
		ghRelease, _ = u.fetchGitHubReleaseByTag(ctx, "nightly")
	} else {
		ghRelease, _ = u.fetchGitHubRelease(ctx, latestTag, channel)
	}
	if ghRelease != nil {
		release.ReleaseNotesURL = ghRelease.HTMLURL
		release.GitHubReleaseURL = ghRelease.HTMLURL
		release.Assets = ghRelease.Assets
	}
	return release, nil
}

func (u *Updater) fetchGitHubReleaseByTag(ctx context.Context, tag string) (*gitHubRelease, error) {
	if strings.TrimSpace(u.cfg.UpdateGitHubRepo) == "" {
		return nil, nil
	}
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", u.cfg.UpdateGitHubRepo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github release %s: status %d", tag, resp.StatusCode)
	}
	var release gitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (u *Updater) fetchDockerTags(ctx context.Context) ([]string, error) {
	parts := strings.SplitN(u.cfg.UpdateImageRepo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid update image repo: %s", u.cfg.UpdateImageRepo)
	}
	reqURL := fmt.Sprintf("https://hub.docker.com/v2/namespaces/%s/repositories/%s/tags?page_size=100", parts[0], parts[1])
	var tags []string
	seen := map[string]bool{}
	for reqURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := u.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("docker hub returned %d", resp.StatusCode)
		}
		var parsed dockerHubTagsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, item := range parsed.Results {
			if !seen[item.Name] {
				tags = append(tags, item.Name)
				seen[item.Name] = true
			}
		}
		if parsed.Next == nil || *parsed.Next == "" {
			break
		}
		reqURL = *parsed.Next
	}
	return tags, nil
}

func (u *Updater) fetchGitHubRelease(ctx context.Context, preferredTag, channel string) (*gitHubRelease, error) {
	if strings.TrimSpace(u.cfg.UpdateGitHubRepo) == "" {
		return nil, nil
	}
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", u.cfg.UpdateGitHubRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases returned %d", resp.StatusCode)
	}
	var releases []gitHubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, err
	}
	normalizedPreferred := strings.TrimPrefix(preferredTag, "v")
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		if normalizedPreferred != "" && tag == normalizedPreferred {
			return &rel, nil
		}
	}
	for _, rel := range releases {
		if channel == "stable" && rel.Prerelease {
			continue
		}
		return &rel, nil
	}
	return nil, nil
}

func normalizeUpdateChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "stable":
		return "stable"
	case "nightly":
		return "nightly"
	default:
		return ""
	}
}

func isPreReleaseTag(tag string) bool {
	lower := strings.ToLower(tag)
	return strings.Contains(lower, "beta") ||
		strings.Contains(lower, "alpha") ||
		strings.Contains(lower, "rc") ||
		strings.Contains(lower, "dev") ||
		strings.Contains(lower, "preview")
}

func selectLatestTag(tags []string, channel string) string {
	if channel == "nightly" {
		return selectLatestNightlyTag(tags)
	}
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" || tag == "latest" || tag == "nightly" {
			continue
		}
		if !versionTagPattern.MatchString(tag) {
			continue
		}
		if isPreReleaseTag(tag) {
			continue
		}
		filtered = append(filtered, tag)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return compareVersions(filtered[i], filtered[j]) > 0
	})
	if len(filtered) == 0 {
		return ""
	}
	return filtered[0]
}

func selectLatestNightlyTag(tags []string) string {
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if nightlyTagPattern.MatchString(tag) {
			filtered = append(filtered, tag)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i] > filtered[j]
	})
	if len(filtered) == 0 {
		return ""
	}
	return filtered[0]
}

var versionTagPattern = regexp.MustCompile(`^v?\d+(?:\.\d+)*(?:-[0-9A-Za-z]+)*$`)

// nightlyTagPattern 匹配 CI 生成的 nightly 镜像 tag。
// 必须与 .github/workflows/docker-publish.yml 中 nightly-$(date -u +'%Y%m%d%H%M%S')-${GITHUB_SHA::7} 的格式保持一致。
var nightlyTagPattern = regexp.MustCompile(`^nightly-\d{14}-[0-9a-f]{7}$`)

func hasUpdateForChannel(channel, current, latest string) bool {
	if channel == "nightly" {
		return current != latest
	}
	return compareVersions(current, latest) < 0
}

func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	pa := parseVersionParts(a)
	pb := parseVersionParts(b)
	maxLen := len(pa)
	if len(pb) > maxLen {
		maxLen = len(pb)
	}
	for i := 0; i < maxLen; i++ {
		av := 0
		bv := 0
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	if isPreReleaseTag(a) && !isPreReleaseTag(b) {
		return -1
	}
	if !isPreReleaseTag(a) && isPreReleaseTag(b) {
		return 1
	}
	return strings.Compare(a, b)
}

func parseVersionParts(version string) []int {
	cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(version)), "v")
	separators := strings.NewReplacer("-", ".", "_", ".", "+", ".")
	chunks := strings.Split(separators.Replace(cleaned), ".")
	out := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		n, err := strconv.Atoi(chunk)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func cloneUpdateStatus(in UpdateStatus) UpdateStatus {
	out := in
	if in.Logs != nil {
		out.Logs = append([]string(nil), in.Logs...)
	}
	return out
}

func (u *Updater) appendLogLocked(message string) {
	ts := time.Now().UTC().Format("15:04:05")
	u.status.Logs = append(u.status.Logs, fmt.Sprintf("%s %s", ts, message))
	if len(u.status.Logs) > 20 {
		u.status.Logs = append([]string(nil), u.status.Logs[len(u.status.Logs)-20:]...)
	}
}

func (u *Updater) persistStateLocked() {
	if u.statePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(u.statePath), 0755); err != nil {
		slog.Warn("create update state dir failed", "error", err)
		return
	}
	raw, err := json.MarshalIndent(u.status, "", "  ")
	if err != nil {
		slog.Warn("marshal update state failed", "error", err)
		return
	}
	tmp := u.statePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		slog.Warn("write update state temp failed", "error", err)
		return
	}
	if err := os.Rename(tmp, u.statePath); err != nil {
		slog.Warn("rename update state failed", "error", err)
	}
}

func (u *Updater) reloadStateLocked() {
	if u.statePath == "" {
		return
	}
	data, err := os.ReadFile(u.statePath)
	if err != nil {
		return
	}
	var parsed UpdateStatus
	if err := json.Unmarshal(data, &parsed); err != nil {
		return
	}
	u.status = parsed
	if u.status.Channel == "" {
		u.status.Channel = defaultUpdateChannel
	}
	if u.status.CurrentVersion == "" {
		u.status.CurrentVersion = u.cfg.Version
	}
}

func (u *Updater) getConfigValue(ctx context.Context, key string) string {
	if u.pool == nil {
		return ""
	}
	var val *string
	if err := u.pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = $1", key).Scan(&val); err != nil || val == nil {
		return ""
	}
	return *val
}

func (u *Updater) setConfigValue(ctx context.Context, key, value string) error {
	if u.pool == nil {
		return nil
	}
	_, err := u.pool.Exec(ctx,
		`INSERT INTO system_config (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value)
	return err
}

func isUpdateTaskActive(status string) bool {
	switch status {
	case "checking", "backing_up", "pulling", "recreating", "rolling_back", "restarting":
		return true
	default:
		return false
	}
}

type dockerClient struct {
	httpClient *http.Client
}

func newDockerClient(socketPath string) *dockerClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerClient{
		httpClient: &http.Client{Transport: transport, Timeout: 0},
	}
}

func (dc *dockerClient) CloseIdleConnections() {
	if tr, ok := dc.httpClient.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

func (dc *dockerClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := dc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return errors.New(msg)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (dc *dockerClient) doStream(ctx context.Context, method, path string) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := dc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return errors.New(msg)
	}
	return nil
}

func (dc *dockerClient) ping(ctx context.Context) error {
	return dc.doStream(ctx, http.MethodGet, "/_ping")
}

type dockerInspect struct {
	ID              string                `json:"Id"`
	Name            string                `json:"Name"`
	Image           string                `json:"Image"`
	Config          dockerContainerConfig `json:"Config"`
	HostConfig      map[string]any        `json:"HostConfig"`
	NetworkSettings dockerNetworkSettings `json:"NetworkSettings"`
	Mounts          []dockerMountPoint    `json:"Mounts"`
}

type dockerContainerConfig struct {
	Image        string              `json:"Image"`
	Env          []string            `json:"Env"`
	Cmd          []string            `json:"Cmd"`
	Entrypoint   []string            `json:"Entrypoint"`
	Labels       map[string]string   `json:"Labels"`
	WorkingDir   string              `json:"WorkingDir"`
	User         string              `json:"User"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	StopSignal   string              `json:"StopSignal"`
	StopTimeout  *int                `json:"StopTimeout"`
	Healthcheck  any                 `json:"Healthcheck"`
	Tty          bool                `json:"Tty"`
	OpenStdin    bool                `json:"OpenStdin"`
	StdinOnce    bool                `json:"StdinOnce"`
	AttachStdin  bool                `json:"AttachStdin"`
	AttachStdout bool                `json:"AttachStdout"`
	AttachStderr bool                `json:"AttachStderr"`
}

type dockerNetworkSettings struct {
	Networks map[string]dockerEndpoint `json:"Networks"`
}

type dockerEndpoint struct {
	Aliases           []string          `json:"Aliases"`
	Links             []string          `json:"Links"`
	IPAddress         string            `json:"IPAddress"`
	GlobalIPv6Address string            `json:"GlobalIPv6Address"`
	MacAddress        string            `json:"MacAddress"`
	DriverOpts        map[string]string `json:"DriverOpts"`
	IPAMConfig        map[string]any    `json:"IPAMConfig"`
}

type dockerMountPoint struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

func (dc *dockerClient) inspectContainer(ctx context.Context, containerID string) (*dockerInspect, error) {
	var out dockerInspect
	if err := dc.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(containerID)+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (dc *dockerClient) createContainer(ctx context.Context, name string, body any) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	path := "/containers/create"
	if name != "" {
		path += "?name=" + url.QueryEscape(name)
	}
	if err := dc.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (dc *dockerClient) startContainer(ctx context.Context, containerID string) error {
	return dc.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(containerID)+"/start", nil, nil)
}

func (dc *dockerClient) stopContainer(ctx context.Context, containerID string, timeoutSec int) error {
	return dc.doJSON(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(containerID), timeoutSec), nil, nil)
}

func (dc *dockerClient) removeContainer(ctx context.Context, containerID string, force bool) error {
	return dc.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/containers/%s?force=%t", url.PathEscape(containerID), force), nil, nil)
}

func (dc *dockerClient) renameContainer(ctx context.Context, containerID, newName string) error {
	return dc.doJSON(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/rename?name=%s", url.PathEscape(containerID), url.QueryEscape(newName)), nil, nil)
}

func (dc *dockerClient) pullImage(ctx context.Context, image string) error {
	repo := image
	tag := "latest"
	if idx := strings.LastIndex(image, ":"); idx > strings.LastIndex(image, "/") {
		repo = image[:idx]
		tag = image[idx+1:]
	}
	return dc.doStream(ctx, http.MethodPost, fmt.Sprintf("/images/create?fromImage=%s&tag=%s", url.QueryEscape(repo), url.QueryEscape(tag)))
}

func currentContainerID() (string, error) {
	if v := strings.TrimSpace(os.Getenv("FYMS_CONTAINER_ID")); v != "" {
		return v, nil
	}
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host), nil
	}
	return "", fmt.Errorf("cannot determine current container id")
}

func buildHelperBinds(socketPath string, inspect *dockerInspect, sharedDestination string) []string {
	binds := []string{fmt.Sprintf("%s:%s", socketPath, socketPath)}
	for _, mount := range inspect.Mounts {
		if mount.Destination != sharedDestination {
			continue
		}
		mode := "rw"
		if !mount.RW {
			mode = "ro"
		}
		source := mount.Source
		if mount.Type == "volume" && mount.Name != "" {
			source = mount.Name
		}
		binds = append(binds, fmt.Sprintf("%s:%s:%s", source, mount.Destination, mode))
		break
	}
	return binds
}

func rollbackImageRef(inspect *dockerInspect) string {
	if inspect == nil {
		return ""
	}
	if strings.TrimSpace(inspect.Image) != "" {
		return strings.TrimSpace(inspect.Image)
	}
	return strings.TrimSpace(inspect.Config.Image)
}

func RunUpdaterRunnerFromEnv() error {
	socketPath := strings.TrimSpace(os.Getenv("FYMS_UPDATE_DOCKER_SOCKET"))
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	action := strings.TrimSpace(os.Getenv("FYMS_UPDATE_ACTION"))
	if action == "" {
		action = "apply"
	}
	targetContainer := strings.TrimSpace(os.Getenv("FYMS_UPDATE_TARGET_CONTAINER"))
	targetImage := strings.TrimSpace(os.Getenv("FYMS_UPDATE_TARGET_IMAGE"))
	targetVersion := strings.TrimSpace(os.Getenv("FYMS_UPDATE_TARGET_VERSION"))
	statePath := strings.TrimSpace(os.Getenv("FYMS_UPDATE_STATE_PATH"))
	if targetContainer == "" || targetImage == "" || statePath == "" {
		return fmt.Errorf("missing updater runner env")
	}

	writeRunnerState(statePath, func(st *UpdateStatus) {
		if action == "rollback" {
			st.Status = "rolling_back"
			st.Message = "正在准备回滚镜像"
			st.RollbackTargetImage = targetImage
			st.RollbackTargetVersion = targetVersion
			appendRunnerLog(st, "开始准备回滚镜像 "+targetImage)
			return
		}
		st.Status = "pulling"
		st.Message = "正在拉取新镜像"
		st.TargetImage = targetImage
		st.TargetVersion = targetVersion
		appendRunnerLog(st, "开始拉取镜像 "+targetImage)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dockerClient := newDockerClient(socketPath)
	defer dockerClient.CloseIdleConnections()

	if action != "rollback" {
		if err := dockerClient.pullImage(ctx, targetImage); err != nil {
			writeRunnerFailure(statePath, fmt.Errorf("pull image failed: %w", err))
			return err
		}
	}
	writeRunnerState(statePath, func(st *UpdateStatus) {
		st.Status = "recreating"
		if action == "rollback" {
			st.Message = "正在回滚容器"
			appendRunnerLog(st, "开始用上一版本镜像重建容器")
		} else {
			st.Message = "正在重建容器"
			appendRunnerLog(st, "镜像拉取完成")
		}
	})

	inspect, err := dockerClient.inspectContainer(ctx, targetContainer)
	if err != nil {
		writeRunnerFailure(statePath, fmt.Errorf("inspect target container failed: %w", err))
		return err
	}
	originalName := strings.TrimPrefix(inspect.Name, "/")
	if originalName == "" {
		originalName = targetContainer[:12]
	}
	backupName := fmt.Sprintf("%s-backup-%d", originalName, time.Now().Unix())

	if err := dockerClient.renameContainer(ctx, targetContainer, backupName); err != nil {
		writeRunnerFailure(statePath, fmt.Errorf("rename current container failed: %w", err))
		return err
	}
	writeRunnerState(statePath, func(st *UpdateStatus) {
		appendRunnerLog(st, "已重命名当前容器为 "+backupName)
	})

	if err := dockerClient.stopContainer(ctx, targetContainer, 15); err != nil {
		updateErr := fmt.Errorf("stop old container failed: %w", err)
		writeRunnerState(statePath, func(st *UpdateStatus) {
			appendRunnerLog(st, "旧容器停止失败，尝试恢复容器名称")
		})
		if rollbackErr := rollbackDockerUpdate(ctx, dockerClient, targetContainer, "", originalName); rollbackErr != nil {
			updateErr = fmt.Errorf("%w; rollback failed: %v", updateErr, rollbackErr)
		}
		writeRunnerFailure(statePath, updateErr)
		return updateErr
	}
	writeRunnerState(statePath, func(st *UpdateStatus) {
		appendRunnerLog(st, "旧容器已停止")
	})

	createBody := buildReplacementContainerBody(inspect, targetImage)
	newID, err := dockerClient.createContainer(ctx, originalName, createBody)
	if err != nil {
		updateErr := fmt.Errorf("create replacement container failed: %w", err)
		writeRunnerState(statePath, func(st *UpdateStatus) {
			appendRunnerLog(st, "新容器创建失败，尝试恢复旧容器")
		})
		if rollbackErr := rollbackDockerUpdate(ctx, dockerClient, targetContainer, "", originalName); rollbackErr != nil {
			updateErr = fmt.Errorf("%w; rollback failed: %v", updateErr, rollbackErr)
		}
		writeRunnerFailure(statePath, updateErr)
		return updateErr
	}
	if err := dockerClient.startContainer(ctx, newID); err != nil {
		updateErr := fmt.Errorf("start replacement container failed: %w", err)
		writeRunnerState(statePath, func(st *UpdateStatus) {
			appendRunnerLog(st, "新容器启动失败，尝试恢复旧容器")
		})
		if rollbackErr := rollbackDockerUpdate(ctx, dockerClient, targetContainer, newID, originalName); rollbackErr != nil {
			updateErr = fmt.Errorf("%w; rollback failed: %v", updateErr, rollbackErr)
		}
		writeRunnerFailure(statePath, updateErr)
		return updateErr
	}

	_ = dockerClient.removeContainer(ctx, targetContainer, false)
	writeRunnerState(statePath, func(st *UpdateStatus) {
		now := time.Now().UTC().Format(time.RFC3339)
		if action == "rollback" {
			st.Status = "restarting"
			st.Message = "回滚完成，服务正在重启"
			st.CurrentVersion = targetVersion
			st.RollbackTargetVersion = targetVersion
			st.RollbackTargetImage = targetImage
			st.TargetVersion = ""
			st.TargetImage = ""
			appendRunnerLog(st, "上一版本容器已启动")
		} else {
			st.Status = "completed"
			st.Message = "更新完成"
			st.CurrentVersion = targetVersion
			appendRunnerLog(st, "新容器已启动")
		}
		st.CompletedAt = &now
		st.Error = nil
		st.HasUpdate = false
	})
	return nil
}

func rollbackDockerUpdate(ctx context.Context, dockerClient *dockerClient, oldContainerID, replacementID, originalName string) error {
	var errs []string
	if replacementID != "" {
		if err := dockerClient.removeContainer(ctx, replacementID, true); err != nil {
			errs = append(errs, "remove replacement container: "+err.Error())
		}
	}
	if originalName != "" {
		if err := dockerClient.renameContainer(ctx, oldContainerID, originalName); err != nil {
			errs = append(errs, "restore old container name: "+err.Error())
		}
	}
	if err := dockerClient.startContainer(ctx, oldContainerID); err != nil {
		if !isContainerAlreadyRunningError(err) {
			errs = append(errs, "start old container: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func isContainerAlreadyRunningError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already started") ||
		strings.Contains(msg, "already running") ||
		strings.Contains(msg, "304")
}

func buildReplacementContainerBody(inspect *dockerInspect, targetImage string) map[string]any {
	hostConfig := cloneMap(inspect.HostConfig)
	hasBinds := anySliceLen(hostConfig["Binds"]) > 0
	if !hasBinds {
		delete(hostConfig, "Binds")
	}
	if hasBinds {
		delete(hostConfig, "Mounts")
	} else if _, ok := hostConfig["Mounts"]; !ok && len(inspect.Mounts) > 0 {
		hostConfig["Mounts"] = buildHostMounts(inspect.Mounts)
	}
	delete(hostConfig, "Links")
	delete(hostConfig, "RestartCount")

	endpoints := map[string]any{}
	for name, endpoint := range inspect.NetworkSettings.Networks {
		entry := map[string]any{}
		if len(endpoint.Aliases) > 0 {
			entry["Aliases"] = endpoint.Aliases
		}
		if len(endpoint.Links) > 0 {
			entry["Links"] = endpoint.Links
		}
		if len(endpoint.DriverOpts) > 0 {
			entry["DriverOpts"] = endpoint.DriverOpts
		}
		endpoints[name] = entry
	}

	return map[string]any{
		"Image":            targetImage,
		"Env":              inspect.Config.Env,
		"Cmd":              inspect.Config.Cmd,
		"Entrypoint":       inspect.Config.Entrypoint,
		"WorkingDir":       inspect.Config.WorkingDir,
		"User":             inspect.Config.User,
		"Labels":           inspect.Config.Labels,
		"ExposedPorts":     inspect.Config.ExposedPorts,
		"StopSignal":       inspect.Config.StopSignal,
		"StopTimeout":      inspect.Config.StopTimeout,
		"Healthcheck":      inspect.Config.Healthcheck,
		"Tty":              inspect.Config.Tty,
		"OpenStdin":        inspect.Config.OpenStdin,
		"StdinOnce":        inspect.Config.StdinOnce,
		"AttachStdin":      inspect.Config.AttachStdin,
		"AttachStdout":     inspect.Config.AttachStdout,
		"AttachStderr":     inspect.Config.AttachStderr,
		"HostConfig":       hostConfig,
		"NetworkingConfig": map[string]any{"EndpointsConfig": endpoints},
	}
}

func anySliceLen(v any) int {
	switch vv := v.(type) {
	case []any:
		return len(vv)
	case []string:
		return len(vv)
	default:
		return 0
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildHostMounts(mounts []dockerMountPoint) []map[string]any {
	out := make([]map[string]any, 0, len(mounts))
	for _, mount := range mounts {
		entry := map[string]any{
			"Type":     mount.Type,
			"Target":   mount.Destination,
			"ReadOnly": !mount.RW,
		}
		if mount.Type == "volume" && mount.Name != "" {
			entry["Source"] = mount.Name
		} else {
			entry["Source"] = mount.Source
		}
		out = append(out, entry)
	}
	return out
}

func writeRunnerFailure(statePath string, err error) {
	writeRunnerState(statePath, func(st *UpdateStatus) {
		now := time.Now().UTC().Format(time.RFC3339)
		st.Status = "failed"
		st.Message = "更新失败"
		st.CompletedAt = &now
		msg := err.Error()
		st.Error = &msg
		appendRunnerLog(st, "更新失败: "+msg)
	})
}

func appendRunnerLog(st *UpdateStatus, message string) {
	ts := time.Now().UTC().Format("15:04:05")
	st.Logs = append(st.Logs, fmt.Sprintf("%s %s", ts, message))
	if len(st.Logs) > 20 {
		st.Logs = append([]string(nil), st.Logs[len(st.Logs)-20:]...)
	}
}

func writeRunnerState(statePath string, mutate func(st *UpdateStatus)) {
	if statePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(statePath), 0755)
	state := UpdateStatus{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	mutate(&state)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, statePath)
}
