//go:build moviepilot
// +build moviepilot

package router

import (
	"sync"
	"time"

	resourceapp "emby-bot-new/internal/application/resource"
)

type resourceSearchStore struct {
	mu      sync.Mutex
	timeout time.Duration
	items   map[int64]resourceSearchSnapshot
}

type resourceSearchSnapshot struct {
	UpdatedAt time.Time
	Items     []resourceapp.Result
}

func newResourceSearchStore(timeout time.Duration) *resourceSearchStore {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &resourceSearchStore{
		timeout: timeout,
		items:   make(map[int64]resourceSearchSnapshot),
	}
}

func (s *resourceSearchStore) Set(userID int64, items []resourceapp.Result) {
	if s == nil || userID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]resourceapp.Result, len(items))
	copy(cp, items)
	s.items[userID] = resourceSearchSnapshot{
		UpdatedAt: time.Now(),
		Items:     cp,
	}
}

func (s *resourceSearchStore) Get(userID int64) ([]resourceapp.Result, bool) {
	if s == nil || userID == 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.items[userID]
	if !ok {
		return nil, false
	}
	if time.Since(snap.UpdatedAt) > s.timeout {
		delete(s.items, userID)
		return nil, false
	}
	cp := make([]resourceapp.Result, len(snap.Items))
	copy(cp, snap.Items)
	return cp, true
}

func (s *resourceSearchStore) Clear(userID int64) {
	if s == nil || userID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, userID)
}
