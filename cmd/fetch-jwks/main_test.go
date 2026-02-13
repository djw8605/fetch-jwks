package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbockelm/fetch-jwks/internal/cache"
	"github.com/bbockelm/fetch-jwks/internal/config"
	"github.com/bbockelm/fetch-jwks/internal/fetcher"
)

func TestRetryThenSuccess(t *testing.T) {
	var calls int32
	jwks := map[string]any{"keys": []map[string]any{{"kid": "k1"}}}
	jwksHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	var srv *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": srv.URL + "/jwks"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/jwks") {
			jwksHandler.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	srv = httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	cfg := config.Config{
		CacheDir:       tmp,
		CacheFile:      filepath.Join(tmp, "cache.json"),
		TTL:            config.Duration{Duration: time.Hour},
		RequestTimeout: config.Duration{Duration: 2 * time.Second},
		Issuers: []config.IssuerConfig{{
			Issuer: srv.URL,
		}},
	}

	client := &http.Client{Timeout: cfg.RequestTimeout.Duration}
	if _, err := fetcher.Run(context.Background(), client, cfg); err != nil {
		t.Fatalf("runOnce failed: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected 2 calls (retry), got %d", calls)
	}

	doc, err := cache.LoadFile(cfg.CacheFile)
	if err != nil {
		t.Fatalf("load cache file: %v", err)
	}
	entry, ok := doc[cfg.Issuers[0].Issuer]
	if !ok {
		t.Fatalf("issuer missing in cache")
	}
	keys, ok := entry.JWKS["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("unexpected jwks keys: %#v", entry.JWKS["keys"])
	}
}

func TestETagNotModifiedUsesCache(t *testing.T) {
	jwks := map[string]any{"keys": []map[string]any{{"kid": "existing"}}}
	cached := cache.BuildEntry(jwks, time.Hour)
	cached.ETag = "abc"
	var calls int32

	tmp := t.TempDir()
	cacheFile := filepath.Join(tmp, "cache.json")
	var srv *httptest.Server
	issuer := ""

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": srv.URL + "/jwks"})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/jwks") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("If-None-Match") != "abc" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	})
	srv = httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	issuer = srv.URL

	if err := cache.WriteFile(cacheFile, cache.Document{issuer: cached}); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cfg := config.Config{
		CacheDir:       tmp,
		CacheFile:      cacheFile,
		TTL:            config.Duration{Duration: time.Hour},
		RequestTimeout: config.Duration{Duration: 2 * time.Second},
		Issuers: []config.IssuerConfig{{
			Issuer: issuer,
		}},
	}

	client := &http.Client{Timeout: cfg.RequestTimeout.Duration}
	before := cached.Expiration
	if _, err := fetcher.Run(context.Background(), client, cfg); err != nil {
		t.Fatalf("runOnce failed: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected discovery + conditional request, got %d", calls)
	}

	doc, err := cache.LoadFile(cacheFile)
	if err != nil {
		t.Fatalf("load cache file: %v", err)
	}
	entry := doc[cfg.Issuers[0].Issuer]
	if entry.ETag != "abc" {
		t.Fatalf("etag mismatch: %s", entry.ETag)
	}
	if entry.Expiration <= before {
		t.Fatalf("expiration did not refresh: before %f after %f", before, entry.Expiration)
	}
}

func TestWritesCacheDirectoryPerIssuer(t *testing.T) {
	keys := map[string][]map[string]any{
		"issuer-a": {{"kid": "a1"}},
		"issuer-b": {{"kid": "b1"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys["issuer-a"]})
		case "/b/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys["issuer-b"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := config.Config{
		CacheDir: cacheDir,
		TTL:      config.Duration{Duration: time.Hour},
		Issuers: []config.IssuerConfig{
			{Issuer: srv.URL + "/issuer-a", JWKSURI: srv.URL + "/a/jwks"},
			{Issuer: srv.URL + "/issuer-b", JWKSURI: srv.URL + "/b/jwks"},
		},
	}

	client := &http.Client{Timeout: 2 * time.Second}
	if _, err := fetcher.Run(context.Background(), client, cfg); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != len(cfg.Issuers) {
		t.Fatalf("expected %d cache files, got %d", len(cfg.Issuers), len(entries))
	}

	expect := map[string]string{}
	for _, iss := range cfg.Issuers {
		expect[iss.Issuer] = hashedName(iss.Issuer)
	}

	for _, e := range entries {
		if _, ok := expectPath(expect, e.Name()); !ok {
			t.Fatalf("unexpected cache file %q", e.Name())
		}
	}

	for issuer, name := range expect {
		path := filepath.Join(cacheDir, name)
		entry, ok, err := cache.LoadHashed(cacheDir, issuer)
		if err != nil {
			t.Fatalf("load hashed %s: %v", issuer, err)
		}
		if !ok {
			t.Fatalf("entry missing for issuer %s", issuer)
		}
		keysVal, ok := entry.JWKS["keys"].([]any)
		if !ok || len(keysVal) != 1 {
			t.Fatalf("issuer %s unexpected keys: %#v", issuer, entry.JWKS["keys"])
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cache file missing: %s", path)
		}
	}
}

func hashedName(issuer string) string {
	sum := sha256.Sum256([]byte(issuer))
	return hex.EncodeToString(sum[:])[:8]
}

func expectPath(expect map[string]string, name string) (string, bool) {
	for issuer, n := range expect {
		if n == name {
			return issuer, true
		}
	}
	return "", false
}
