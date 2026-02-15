package router

import (
	"sync"
	"time"
)

type uiStore struct {
	mu      sync.Mutex
	timeout time.Duration
	items   map[int64]uiAnchor
}

type uiAnchor struct {
	MessageID int
	IsMedia   bool
	UpdatedAt time.Time
}

func newUIStore(timeout time.Duration) *uiStore {
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}
	return &uiStore{
		timeout: timeout,
		items:   make(map[int64]uiAnchor),
	}
}

func (s *uiStore) Set(userID int64, messageID int, isMedia bool) {
	if s == nil || userID == 0 || messageID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[userID] = uiAnchor{
		MessageID: messageID,
		IsMedia:   isMedia,
		UpdatedAt: time.Now(),
	}
}

func (s *uiStore) Get(userID int64) (uiAnchor, bool) {
	if s == nil || userID == 0 {
		return uiAnchor{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[userID]
	if !ok {
		return uiAnchor{}, false
	}
	if time.Since(it.UpdatedAt) > s.timeout {
		delete(s.items, userID)
		return uiAnchor{}, false
	}
	if it.MessageID <= 0 {
		return uiAnchor{}, false
	}
	return it, true
}
