package services

import (
	"sync"
	"time"
)

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
}

type NowPlaying struct {
	ItemID             string  `json:"ItemId"`
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
	for k, s := range sm.sessions {
		if s.LastActivity.Before(cutoff) {
			delete(sm.sessions, k)
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
		s.LastActivity = time.Now()
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
