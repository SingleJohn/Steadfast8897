package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

func (u *Updater) ListVersions(ctx context.Context, channel string) (UpdateVersionsResponse, error) {
	channel = normalizeUpdateChannel(channel)
	if channel == "" {
		channel = normalizeUpdateChannel(u.getConfigValue(ctx, updateChannelKey))
	}
	if channel == "" {
		channel = defaultUpdateChannel
	}

	mode := DetectDeploymentMode()
	resp := UpdateVersionsResponse{
		Channel:        channel,
		CurrentVersion: u.cfg.Version,
		DeploymentMode: string(mode),
		Versions:       []UpdateVersion{},
	}
	switch mode {
	case DeployDocker:
		versions, err := u.listDockerVersions(ctx, channel)
		if err != nil {
			return resp, err
		}
		resp.Versions = versions
	case DeployBinary:
		versions, err := u.listBinaryVersions(ctx, channel)
		if err != nil {
			return resp, err
		}
		resp.Versions = versions
	case DeployManual:
		versions, err := u.listBinaryVersions(ctx, channel)
		if err != nil {
			return resp, err
		}
		for i := range versions {
			versions[i].Installable = false
			versions[i].Reason = "当前平台不支持应用内自动切换版本"
		}
		resp.Versions = versions
	default:
		return resp, fmt.Errorf("unknown deployment mode")
	}
	return resp, nil
}

func (u *Updater) resolveDockerReleaseVersion(ctx context.Context, channel, version string) (UpdateRelease, error) {
	channel = normalizeUpdateChannel(channel)
	if channel == "" {
		return UpdateRelease{}, fmt.Errorf("invalid update channel")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return UpdateRelease{}, fmt.Errorf("version is required")
	}
	tags, err := u.fetchDockerTags(ctx)
	if err != nil {
		return UpdateRelease{}, err
	}
	if !containsVersionTag(tags, version, channel) {
		return UpdateRelease{}, fmt.Errorf("version %s is not available in %s channel", version, channel)
	}
	release := UpdateRelease{
		Version:       version,
		Channel:       channel,
		Image:         fmt.Sprintf("%s:%s", u.cfg.UpdateImageRepo, version),
		ReleaseSource: "docker",
	}
	var ghRelease *gitHubRelease
	if channel == "nightly" {
		ghRelease, _ = u.fetchGitHubReleaseByTag(ctx, "nightly")
	} else {
		ghRelease, _ = u.fetchGitHubRelease(ctx, version, channel)
	}
	if ghRelease != nil {
		release.ReleaseNotesURL = ghRelease.HTMLURL
		release.GitHubReleaseURL = ghRelease.HTMLURL
		release.Assets = ghRelease.Assets
	}
	return release, nil
}

func (u *Updater) resolveBinaryReleaseVersion(ctx context.Context, channel, version string) (UpdateRelease, error) {
	channel = normalizeUpdateChannel(channel)
	if channel == "" {
		return UpdateRelease{}, fmt.Errorf("invalid update channel")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return UpdateRelease{}, fmt.Errorf("version is required")
	}
	var ghRelease *gitHubRelease
	var err error
	if channel == "nightly" {
		ghRelease, err = u.fetchGitHubReleaseByTag(ctx, "nightly")
	} else {
		ghRelease, err = u.fetchGitHubReleaseByTag(ctx, version)
	}
	if err != nil {
		return UpdateRelease{}, err
	}
	if ghRelease == nil {
		return UpdateRelease{}, fmt.Errorf("github release %s not found", version)
	}
	releaseVersion := version
	if channel == "nightly" && ghRelease.TagName != "" {
		releaseVersion = ghRelease.TagName
	}
	return UpdateRelease{
		Version:          releaseVersion,
		Channel:          channel,
		ReleaseSource:    "github",
		ReleaseNotesURL:  ghRelease.HTMLURL,
		GitHubReleaseURL: ghRelease.HTMLURL,
		Assets:           ghRelease.Assets,
	}, nil
}

func (u *Updater) listDockerVersions(ctx context.Context, channel string) ([]UpdateVersion, error) {
	tags, err := u.fetchDockerTags(ctx)
	if err != nil {
		return nil, err
	}
	filtered := filterVersionTags(tags, channel)
	return buildUpdateVersions(filtered, channel, u.cfg.Version, func(version string) UpdateVersion {
		return UpdateVersion{
			Version:       version,
			Channel:       channel,
			Image:         fmt.Sprintf("%s:%s", u.cfg.UpdateImageRepo, version),
			ReleaseSource: "docker",
			Installable:   true,
		}
	}), nil
}

func (u *Updater) listBinaryVersions(ctx context.Context, channel string) ([]UpdateVersion, error) {
	if strings.TrimSpace(u.cfg.UpdateGitHubRepo) == "" {
		return nil, fmt.Errorf("github repo is not configured")
	}
	if channel == "nightly" {
		rel, err := u.fetchGitHubReleaseByTag(ctx, "nightly")
		if err != nil {
			return nil, err
		}
		version := "nightly"
		if rel != nil && rel.TagName != "" {
			version = rel.TagName
		}
		return buildUpdateVersions([]string{version}, channel, u.cfg.Version, func(v string) UpdateVersion {
			item := UpdateVersion{
				Version:       v,
				Channel:       channel,
				ReleaseSource: "github",
				Installable:   rel != nil && binaryReleaseHasCurrentAsset(rel),
			}
			if rel != nil {
				item.GitHubReleaseURL = rel.HTMLURL
				item.ReleaseNotesURL = rel.HTMLURL
			}
			if !item.Installable {
				item.Reason = "当前平台没有可用安装包"
			}
			return item
		}), nil
	}
	releases, err := u.fetchGitHubReleases(ctx, 30)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(releases))
	releaseByTag := map[string]*gitHubRelease{}
	for i := range releases {
		rel := &releases[i]
		if rel.Prerelease {
			continue
		}
		tag := strings.TrimSpace(rel.TagName)
		if tag == "" || !versionTagPattern.MatchString(tag) || isPreReleaseTag(tag) {
			continue
		}
		tags = append(tags, tag)
		releaseByTag[tag] = rel
	}
	sort.Slice(tags, func(i, j int) bool {
		return compareVersions(tags[i], tags[j]) > 0
	})
	return buildUpdateVersions(tags, channel, u.cfg.Version, func(version string) UpdateVersion {
		rel := releaseByTag[version]
		item := UpdateVersion{
			Version:       version,
			Channel:       channel,
			ReleaseSource: "github",
			Installable:   rel != nil && binaryReleaseHasCurrentAsset(rel),
		}
		if rel != nil {
			item.GitHubReleaseURL = rel.HTMLURL
			item.ReleaseNotesURL = rel.HTMLURL
		}
		if !item.Installable {
			item.Reason = "当前平台没有可用安装包"
		}
		return item
	}), nil
}

func (u *Updater) fetchGitHubReleases(ctx context.Context, perPage int) ([]gitHubRelease, error) {
	if strings.TrimSpace(u.cfg.UpdateGitHubRepo) == "" {
		return nil, nil
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 30
	}
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", u.cfg.UpdateGitHubRepo, perPage)
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
	return releases, nil
}

func filterVersionTags(tags []string, channel string) []string {
	filtered := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		if tag == "" || seen[tag] {
			continue
		}
		if channel == "nightly" {
			if nightlyTagPattern.MatchString(tag) {
				filtered = append(filtered, tag)
				seen[tag] = true
			}
			continue
		}
		if tag == "latest" || tag == "nightly" {
			continue
		}
		if !versionTagPattern.MatchString(tag) || isPreReleaseTag(tag) {
			continue
		}
		filtered = append(filtered, tag)
		seen[tag] = true
	}
	if channel == "nightly" {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i] > filtered[j]
		})
	} else {
		sort.Slice(filtered, func(i, j int) bool {
			return compareVersions(filtered[i], filtered[j]) > 0
		})
	}
	if len(filtered) > 30 {
		filtered = filtered[:30]
	}
	return filtered
}

func containsVersionTag(tags []string, version, channel string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	for _, tag := range filterVersionTags(tags, channel) {
		if tag == version {
			return true
		}
	}
	return false
}

func buildUpdateVersions(tags []string, channel, current string, makeItem func(string) UpdateVersion) []UpdateVersion {
	out := make([]UpdateVersion, 0, len(tags))
	for _, tag := range tags {
		item := makeItem(tag)
		item.Version = tag
		item.Channel = channel
		item.Current = tag == current
		item.Direction = updateDirection(channel, current, tag)
		if item.Current {
			item.Direction = "current"
			item.Installable = false
			item.Reason = "当前正在运行此版本"
		}
		out = append(out, item)
	}
	return out
}

func updateDirection(channel, current, target string) string {
	if current == target {
		return "current"
	}
	if channel == "nightly" {
		if current < target {
			return "upgrade"
		}
		return "downgrade"
	}
	if compareVersions(current, target) < 0 {
		return "upgrade"
	}
	return "downgrade"
}

func binaryReleaseHasCurrentAsset(rel *gitHubRelease) bool {
	if rel == nil {
		return false
	}
	target := BuildTargetName()
	if target == "" {
		return false
	}
	ext := BuildArchiveExt()
	for _, asset := range rel.Assets {
		if strings.Contains(asset.Name, "_"+target+"."+ext) {
			return true
		}
	}
	return false
}
