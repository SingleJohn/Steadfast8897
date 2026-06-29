package services

import (
	"sync"
	"time"
)

// nowPlayingTimeout 是 NowPlaying 的存活窗口:超过这段时间没有播放进度刷新
// (SetNowPlaying)就认为该流已经停止。用于防止客户端异常退出未发 Stopped 事件、
// 但仍在浏览导致会话 LastActivity 持续刷新时,僵尸 NowPlaying 一直占用并发流名额。
// 与 handlers 内 activePlaybacks 的 120s 回收阈值保持一致。
const nowPlayingTimeout = 2 * time.Minute

type ActiveSession struct {
	UserID        string      `json:"UserId"`
	UserName      string      `json:"UserName"`
	DeviceID      string      `json:"DeviceId"`
	DeviceName    string      `json:"DeviceName"`
	AppName       string      `json:"AppName"`
	AppVersion    string      `json:"AppVersion"`
	ClientIP      string      `json:"ClientIp"`
	PlaySessionID string      `json:"PlaySessionId,omitempty"`
	LastActivity  time.Time   `json:"LastActivity"`
	NowPlaying    *NowPlaying `json:"NowPlaying,omitempty"`
	// NowPlayingAt 记录 NowPlaying 最近一次被设置/刷新的时间,用于过期判定。
	NowPlayingAt time.Time `json:"-"`
}

type NowPlaying struct {
	ItemID             string  `json:"ItemId"`
	MediaSourceID      string  `json:"MediaSourceId,omitempty"`
	ItemName           string  `json:"ItemName"`
	ItemType           string  `json:"ItemType"`
	SeriesName         *string `json:"SeriesName,omitempty"`
	RuntimeTicks       *int64  `json:"RuntimeTicks,omitempty"`
	PositionTicks      int64   `json:"PositionTicks"`
	IsPaused           bool    `json:"IsPaused"`
	SeasonIndex        *int32  `json:"SeasonIndex,omitempty"`
	EpisodeIndex       *int32  `json:"EpisodeIndex,omitempty"`
	PrimaryImageItemID *string `json:"PrimaryImageItemId,omitempty"`
	PlaySessionID      string  `json:"PlaySessionId,omitempty"`
	PlayMethod         string  `json:"PlayMethod,omitempty"`
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ActiveSession
	stopCh   chan struct{}
}

func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*ActiveSession),
		stopCh:   make(chan struct{}),
	}

	go sm.cleanupLoop()
	return sm
}

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sm.cleanup()
		case <-sm.stopCh:
			return
		}
	}
}

func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	playingCutoff := time.Now().Add(-nowPlayingTimeout)
	for k, s := range sm.sessions {
		if s.LastActivity.Before(cutoff) {
			delete(sm.sessions, k)
			continue
		}
		// 会话本身还活跃,但 NowPlaying 已过期(久未刷新进度)→ 清掉播放状态,
		// 释放并发流名额。
		if s.NowPlaying != nil && s.NowPlayingAt.Before(playingCutoff) {
			s.NowPlaying = nil
			s.PlaySessionID = ""
		}
	}
}

func (sm *SessionManager) UpdateSession(userID, userName, deviceID, deviceName, appName, appVersion, clientIP string) {
	key := userID + ":" + deviceID
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.sessions[key]; ok {
		s.UserName = userName
		s.DeviceName = deviceName
		s.AppName = appName
		if appVersion != "" {
			s.AppVersion = appVersion
		}
		if clientIP != "" {
			s.ClientIP = clientIP
		}
		s.LastActivity = time.Now()
	} else {
		sm.sessions[key] = &ActiveSession{
			UserID:       userID,
			UserName:     userName,
			DeviceID:     deviceID,
			DeviceName:   deviceName,
			AppName:      appName,
			AppVersion:   appVersion,
			ClientIP:     clientIP,
			LastActivity: time.Now(),
		}
	}
}

func (sm *SessionManager) SetNowPlaying(userID, deviceID string, np *NowPlaying) {
	key := userID + ":" + deviceID
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.sessions[key]; ok {
		s.NowPlaying = np
		if np != nil && np.PlaySessionID != "" {
			s.PlaySessionID = np.PlaySessionID
		}
		now := time.Now()
		if np != nil {
			s.NowPlayingAt = now
		}
		s.LastActivity = now
	}
}

func (sm *SessionManager) ClearNowPlaying(userID, deviceID string) {
	key := userID + ":" + deviceID
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.sessions[key]; ok {
		s.NowPlaying = nil
		s.PlaySessionID = ""
	}
}

func (sm *SessionManager) ClearNowPlayingBySessionID(sessionID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, s := range sm.sessions {
		if sessionMatchesID(s, sessionID) {
			s.NowPlaying = nil
			s.PlaySessionID = ""
			s.LastActivity = time.Now()
			return true
		}
	}
	return false
}

func (sm *SessionManager) HasSession(sessionID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for _, s := range sm.sessions {
		if s.LastActivity.After(cutoff) && sessionMatchesID(s, sessionID) {
			return true
		}
	}
	return false
}

func sessionMatchesID(s *ActiveSession, sessionID string) bool {
	if s == nil || sessionID == "" {
		return false
	}
	if s.UserID+"_"+s.DeviceID == sessionID {
		return true
	}
	if s.PlaySessionID == sessionID {
		return true
	}
	return s.NowPlaying != nil && s.NowPlaying.PlaySessionID == sessionID
}

// CountActiveStreams 统计某用户当前真正在播放的并发流数量。只计入 NowPlaying
// 仍新鲜(最近 nowPlayingTimeout 内有进度刷新)且会话活跃的流,读取时即过滤,
// 不依赖后台 cleanup 的 30s 周期,避免僵尸会话误占名额导致错误拦截。
func (sm *SessionManager) CountActiveStreams(userID string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	now := time.Now()
	sessionCutoff := now.Add(-10 * time.Minute)
	playingCutoff := now.Add(-nowPlayingTimeout)
	n := 0
	for _, s := range sm.sessions {
		if s.UserID != userID || s.NowPlaying == nil {
			continue
		}
		if s.LastActivity.After(sessionCutoff) && s.NowPlayingAt.After(playingCutoff) {
			n++
		}
	}
	return n
}

func (sm *SessionManager) GetActiveSessions() []ActiveSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	var result []ActiveSession
	for _, s := range sm.sessions {
		if s.LastActivity.After(cutoff) {
			result = append(result, *s)
		}
	}
	return result
}

func (sm *SessionManager) Stop() {
	close(sm.stopCh)
}
