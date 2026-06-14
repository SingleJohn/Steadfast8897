package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (u *Updater) StartRollback(ctx context.Context) (UpdateStatus, error) {
	mode := DetectDeploymentMode()
	switch mode {
	case DeployDocker:
		return u.startRollbackDocker(ctx)
	case DeployBinary:
		return u.startRollbackBinary(ctx)
	case DeployManual:
		return u.GetStatus(ctx), fmt.Errorf("platform does not support auto rollback")
	default:
		return u.GetStatus(ctx), fmt.Errorf("unknown deployment mode")
	}
}

func (u *Updater) startRollbackDocker(ctx context.Context) (UpdateStatus, error) {
	u.mu.Lock()
	u.reloadStateLocked()
	if isUpdateTaskActive(u.status.Status) {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("update task already running")
	}
	previousImage := strings.TrimSpace(u.status.PreviousImage)
	previousVersion := strings.TrimSpace(u.status.PreviousVersion)
	if previousImage == "" {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("no docker image available for rollback")
	}
	u.mu.Unlock()

	if _, err := os.Stat(u.cfg.UpdateDockerSocket); err != nil {
		return u.GetStatus(ctx), fmt.Errorf("docker socket unavailable: %w", err)
	}

	dockerClient := newDockerClient(u.cfg.UpdateDockerSocket)
	defer dockerClient.CloseIdleConnections()

	containerID, err := currentContainerID()
	if err != nil {
		return u.GetStatus(ctx), err
	}
	inspect, err := dockerClient.inspectContainer(ctx, containerID)
	if err != nil {
		return u.GetStatus(ctx), fmt.Errorf("inspect current container: %w", err)
	}
	currentImage := rollbackImageRef(inspect)

	helperName := fmt.Sprintf("fyms-rollback-%d", time.Now().Unix())
	helperBinds := buildHelperBinds(u.cfg.UpdateDockerSocket, inspect, defaultSharedDataMount)
	helperEnv := []string{
		"FYMS_UPDATE_RUNNER=1",
		"FYMS_UPDATE_ACTION=rollback",
		fmt.Sprintf("FYMS_UPDATE_DOCKER_SOCKET=%s", u.cfg.UpdateDockerSocket),
		fmt.Sprintf("FYMS_UPDATE_TARGET_CONTAINER=%s", containerID),
		fmt.Sprintf("FYMS_UPDATE_TARGET_IMAGE=%s", previousImage),
		fmt.Sprintf("FYMS_UPDATE_TARGET_VERSION=%s", previousVersion),
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
		return u.GetStatus(ctx), fmt.Errorf("create rollback helper: %w", err)
	}
	if err := dockerClient.startContainer(ctx, helperID); err != nil {
		return u.GetStatus(ctx), fmt.Errorf("start rollback helper: %w", err)
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.reloadStateLocked()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "restarting"
	u.status.Message = "回滚任务已启动，服务即将重启"
	u.status.HelperContainer = helperName
	u.status.StartedAt = &now
	u.status.CompletedAt = nil
	u.status.Error = nil
	u.status.CurrentImage = currentImage
	u.status.RollbackTargetImage = previousImage
	u.status.RollbackTargetVersion = previousVersion
	u.status.TargetImage = ""
	u.status.TargetVersion = ""
	u.appendLogLocked(fmt.Sprintf("回滚助手已启动: %s", helperName))
	u.persistStateLocked()
	return cloneUpdateStatus(u.status), nil
}

func (u *Updater) startRollbackBinary(ctx context.Context) (UpdateStatus, error) {
	u.mu.Lock()
	u.reloadStateLocked()
	if isUpdateTaskActive(u.status.Status) {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("update task already running")
	}
	previousVersion := strings.TrimSpace(u.status.PreviousVersion)
	backupPath := u.binaryBackupPath(previousVersion)
	if backupPath == "" {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("no binary backup available for rollback")
	}
	u.mu.Unlock()

	if _, err := os.Stat(backupPath); err != nil {
		return u.GetStatus(ctx), fmt.Errorf("binary backup unavailable: %w", err)
	}
	currentExe, err := os.Executable()
	if err != nil {
		return u.GetStatus(ctx), fmt.Errorf("locate current executable: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(currentExe); rerr == nil {
		currentExe = resolved
	}

	u.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "rolling_back"
	u.status.Message = "正在回滚二进制"
	u.status.StartedAt = &now
	u.status.CompletedAt = nil
	u.status.Error = nil
	u.status.RollbackTargetVersion = previousVersion
	u.status.RollbackTargetImage = ""
	u.appendLogLocked(fmt.Sprintf("开始回滚到 %s", previousVersion))
	u.persistStateLocked()
	u.mu.Unlock()

	currentBackupPath := u.binaryBackupPath(u.cfg.Version)
	if currentBackupPath != "" {
		_ = copyFile(currentExe, currentBackupPath, 0755)
	}
	if err := copyFile(backupPath, currentExe, 0755); err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("restore binary backup: %w", err))
	}

	u.mu.Lock()
	nowR := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "restarting"
	u.status.Message = "二进制回滚完成,即将重启"
	u.status.StartedAt = &nowR
	u.appendLogLocked("二进制已回滚,准备 exec 自替换")
	u.persistStateLocked()
	u.mu.Unlock()

	go func() {
		time.Sleep(300 * time.Millisecond)
		argv := append([]string(nil), os.Args...)
		if err := execSelf(argv, os.Environ()); err != nil {
			slog.Error("exec self after rollback failed", "error", err)
			u.MarkFailure(fmt.Errorf("exec self after rollback: %w", err))
		}
	}()

	return u.GetStatus(ctx), nil
}
