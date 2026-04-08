package service

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ViewerOpts struct {
	ID           string
	Username     string
	ChannelID    string
	ChannelName  string
	StreamID     string
	StreamName   string
	M3UAccountID string
	ProfileName  string
	UserAgent    string
	ClientName   string
	RemoteAddr   string
	Type         string
}

type viewerEntry struct {
	ViewerOpts
	startedAt  time.Time
	lastActive time.Time
}

type userSession struct {
	UserID     string
	Username   string
	Source     string
	RemoteAddr string
	UserAgent  string
	FirstSeen  time.Time
	LastSeen   time.Time
}

type ActivityService struct {
	mu       sync.RWMutex
	viewers  map[string]*viewerEntry
	sessions map[string]*userSession
}

func NewActivityService() *ActivityService {
	return &ActivityService{
		viewers:  make(map[string]*viewerEntry),
		sessions: make(map[string]*userSession),
	}
}

func (s *ActivityService) TouchUser(userID, username, source, remoteAddr, userAgent string) {
	key := userID + ":" + source
	s.mu.Lock()
	now := time.Now()
	if sess, ok := s.sessions[key]; ok {
		sess.LastSeen = now
		sess.RemoteAddr = remoteAddr
		sess.UserAgent = userAgent
	} else {
		s.sessions[key] = &userSession{
			UserID:     userID,
			Username:   username,
			Source:     source,
			RemoteAddr: remoteAddr,
			UserAgent:  userAgent,
			FirstSeen:  now,
			LastSeen:   now,
		}
	}
	s.mu.Unlock()
}

func (s *ActivityService) Add(opts ViewerOpts) string {
	if opts.ID == "" {
		opts.ID = uuid.New().String()
	}
	now := time.Now()
	s.mu.Lock()
	s.viewers[opts.ID] = &viewerEntry{
		ViewerOpts: opts,
		startedAt:  now,
		lastActive: now,
	}
	s.mu.Unlock()
	return opts.ID
}

func (s *ActivityService) SetM3UAccountID(id string, accountID string) {
	s.mu.Lock()
	if v, ok := s.viewers[id]; ok {
		v.M3UAccountID = accountID
	}
	s.mu.Unlock()
}

func (s *ActivityService) Touch(id string) {
	s.mu.Lock()
	if v, ok := s.viewers[id]; ok {
		v.lastActive = time.Now()
	}
	s.mu.Unlock()
}

func (s *ActivityService) Remove(id string) {
	s.mu.Lock()
	delete(s.viewers, id)
	s.mu.Unlock()
}

func (s *ActivityService) List() []models.ActiveViewer {
	s.mu.RLock()
	now := time.Now()
	sessionTimeout := 20 * time.Minute

	list := make([]models.ActiveViewer, 0, len(s.viewers)+len(s.sessions))

	for _, sess := range s.sessions {
		idle := now.Sub(sess.LastSeen)
		if idle > sessionTimeout {
			continue
		}
		list = append(list, models.ActiveViewer{
			ID:         sess.UserID,
			Username:   sess.Username,
			ClientName: sess.Source,
			RemoteAddr: sess.RemoteAddr,
			UserAgent:  sess.UserAgent,
			StartedAt:  sess.FirstSeen.Format(time.RFC3339),
			LastActive: sess.LastSeen.Format(time.RFC3339),
			IdleSecs:   idle.Seconds(),
			Type:       "session",
		})
	}

	for _, v := range s.viewers {
		list = append(list, models.ActiveViewer{
			ID:           v.ID,
			Username:     v.Username,
			ChannelID:    v.ChannelID,
			ChannelName:  v.ChannelName,
			StreamID:     v.StreamID,
			StreamName:   v.StreamName,
			M3UAccountID: v.M3UAccountID,
			ProfileName:  v.ProfileName,
			UserAgent:    v.UserAgent,
			ClientName:   v.ClientName,
			RemoteAddr:   v.RemoteAddr,
			StartedAt:    v.startedAt.Format(time.RFC3339),
			LastActive:   v.lastActive.Format(time.RFC3339),
			IdleSecs:     now.Sub(v.lastActive).Seconds(),
			Type:         v.Type,
		})
	}
	s.mu.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartedAt < list[j].StartedAt
	})
	return list
}

func (s *ActivityService) CountByM3UAccount() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	for _, v := range s.viewers {
		if v.M3UAccountID != "" {
			counts[v.M3UAccountID]++
		}
	}
	return counts
}
