package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/bbockelm/fetch-jwks/internal/config"
)

func TestWellKnownPaths(t *testing.T) {
	issuer := "https://example.org/foo"
	oidc := oidcWellKnown(mustNormalize(t, issuer))
	if oidc != "https://example.org/foo/.well-known/openid-configuration" {
		t.Fatalf("unexpected oidc path: %s", oidc)
	}

	oauth := oauthWellKnown(mustNormalize(t, issuer))
	if oauth != "https://example.org/.well-known/oauth-authorization-server/foo" {
		t.Fatalf("unexpected oauth path: %s", oauth)
	}
}

func TestDiscoveryPrefersOIDCThenOAuth(t *testing.T) {
	var calls []string
	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/foo/.well-known/openid-configuration":
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/oauth-authorization-server/foo":
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": baseURL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	baseURL = srv.URL

	cfg := config.Config{
		Issuers:   []config.IssuerConfig{{Issuer: srv.URL + "/foo"}},
		TTL:       config.Duration{Duration: 0},
		CacheDir:  t.TempDir(),
		CacheFile: filepath.Join(t.TempDir(), "agg.json"),
	}
	client := &http.Client{}
	if _, err := Run(context.Background(), client, cfg); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(calls) == 0 || calls[0] != "/foo/.well-known/openid-configuration" {
		t.Fatalf("expected OIDC path first, got %v", calls)
	}
	if len(calls) < 2 || calls[1] != "/.well-known/oauth-authorization-server/foo" {
		t.Fatalf("expected OAuth fallback second, got %v", calls)
	}
}

func TestRejectsMalformedJWKS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": "not-an-array"})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		Issuers:   []config.IssuerConfig{{Issuer: srv.URL, JWKSURI: srv.URL}},
		TTL:       config.Duration{Duration: 0},
		CacheDir:  t.TempDir(),
		CacheFile: filepath.Join(t.TempDir(), "agg.json"),
	}
	client := &http.Client{}
	if _, err := Run(context.Background(), client, cfg); err == nil {
		t.Fatalf("expected malformed jwks to error")
	}
}

func mustNormalize(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := normalizeIssuer(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return u
}
