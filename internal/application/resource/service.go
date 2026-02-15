package resource

import (
	"context"
	"errors"
	"strings"

	"emby-bot-new/internal/infrastructure/moviepilot"
)

var (
	ErrInvalidQuery = errors.New("invalid query")
	ErrPushDisabled = errors.New("moviepilot push disabled")
)

type Result = moviepilot.Result

type Searcher interface {
	SearchTitle(ctx context.Context, keyword string, limit int) ([]moviepilot.Result, error)
	Check(ctx context.Context) (int, error)
}

type Pusher interface {
	PushDownload(ctx context.Context, link string) error
}

type Service interface {
	Search(ctx context.Context, keyword string) ([]moviepilot.Result, error)
	Push(ctx context.Context, link string) error
	PushEnabled() bool
	Check(ctx context.Context) (int, error)
}

type serviceImpl struct {
	client     Searcher
	pusher     Pusher
	maxResults int
}

func NewService(client Searcher, maxResults int, pushEnabled bool) Service {
	if maxResults <= 0 {
		maxResults = 8
	}
	var pusher Pusher
	if pushEnabled {
		if v, ok := any(client).(Pusher); ok {
			pusher = v
		}
	}
	return &serviceImpl{
		client:     client,
		pusher:     pusher,
		maxResults: maxResults,
	}
}

func (s *serviceImpl) Search(ctx context.Context, keyword string) ([]moviepilot.Result, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("resource service not initialized")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, ErrInvalidQuery
	}
	return s.client.SearchTitle(ctx, keyword, s.maxResults)
}

func (s *serviceImpl) Push(ctx context.Context, link string) error {
	if s == nil || s.client == nil {
		return errors.New("resource service not initialized")
	}
	if s.pusher == nil {
		return ErrPushDisabled
	}
	link = strings.TrimSpace(link)
	if link == "" {
		return ErrInvalidQuery
	}
	return s.pusher.PushDownload(ctx, link)
}

func (s *serviceImpl) PushEnabled() bool {
	return s != nil && s.pusher != nil
}

func (s *serviceImpl) Check(ctx context.Context) (int, error) {
	if s == nil || s.client == nil {
		return 0, errors.New("resource service not initialized")
	}
	return s.client.Check(ctx)
}
