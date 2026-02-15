package moviepilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_SearchTitle(t *testing.T) {
	t.Run("arrayResponse", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/search/title" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if got := r.URL.Query().Get("token"); got == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"title": "A", "site": "S1", "size": "1GB", "seeders": 10, "leechers": 2, "download_url": "x"},
				{"name": "B", "site_name": "S2", "size_str": "2GB", "seeds": "3", "peers": "4", "url": "y"},
			})
		}))
		defer srv.Close()

		c, err := New(Options{BaseURL: srv.URL, APIToken: "t", HTTPTimeout: time.Second, MaxResults: 5})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		got, err := c.SearchTitle(context.Background(), "test", 2)
		if err != nil {
			t.Fatalf("SearchTitle: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d", len(got))
		}
		if got[0].Title != "A" || got[1].Title != "B" {
			t.Fatalf("unexpected titles: %#v", got)
		}
	})

	t.Run("objectWithDataResponse", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"total": 1,
					"list": []map[string]any{
						{"torrent_name": "C", "tracker": "S3"},
					},
				},
			})
		}))
		defer srv.Close()

		c, err := New(Options{BaseURL: srv.URL})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		got, err := c.SearchTitle(context.Background(), "x", 1)
		if err != nil {
			t.Fatalf("SearchTitle: %v", err)
		}
		if len(got) != 1 || got[0].Title != "C" {
			t.Fatalf("unexpected: %#v", got)
		}
	})
}

func TestClient_PushDownload(t *testing.T) {
	t.Run("defaultPath", func(t *testing.T) {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path != "/api/v1/download" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c, err := New(Options{BaseURL: srv.URL, APIToken: "t", HTTPTimeout: time.Second})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := c.PushDownload(context.Background(), "magnet:?xt=urn:btih:xxx"); err != nil {
			t.Fatalf("PushDownload: %v", err)
		}
		if !called {
			t.Fatalf("expected server called")
		}
	})
}

func TestClient_Check(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/search/title" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c, err := New(Options{BaseURL: srv.URL})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		code, err := c.Check(context.Background())
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
	})
}
