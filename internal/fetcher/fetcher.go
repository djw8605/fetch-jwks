package fetcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/bbockelm/fetch-jwks/internal/cache"
	"github.com/bbockelm/fetch-jwks/internal/config"
)

// Run fetches JWKS documents for all configured issuers, respecting concurrency limits.
func Run(ctx context.Context, client *http.Client, cfg config.Config) (cache.Document, error) {
	cfg = cfg.Defaulted()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	existing := make(cache.Document)
	if cfg.CacheFile != "" {
		if doc, err := cache.LoadFile(cfg.CacheFile); err == nil {
			mergeDocs(existing, doc)
		}
	}

	lim := newLimiter(cfg.MaxParallel, cfg.MaxPerHost)
	doc := make(cache.Document)
	var mu sync.Mutex

	type result struct {
		issuer string
		entry  cache.Entry
		err    error
	}

	results := make(chan result, len(cfg.Issuers))
	var wg sync.WaitGroup

	for _, iss := range cfg.Issuers {
		iss := iss
		wg.Add(1)
		go func() {
			defer wg.Done()

			jwksURI, err := resolveJWKSURI(ctx, client, iss)
			if err != nil {
				results <- result{issuer: iss.Issuer, err: err}
				return
			}

			host, err := hostFromURL(jwksURI)
			if err != nil {
				results <- result{issuer: iss.Issuer, err: err}
				return
			}
			release := lim.acquire(host)
			defer release()

			cached, hasCached, err := cachedEntry(cfg, iss.Issuer, existing)
			if err != nil {
				results <- result{issuer: iss.Issuer, err: err}
				return
			}
			entry, err := fetchIssuerWithRetry(ctx, client, jwksURI, cfg.TTL.Duration, cached, hasCached)
			if err != nil {
				results <- result{issuer: iss.Issuer, err: err}
				return
			}
			results <- result{issuer: iss.Issuer, entry: entry}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		mu.Lock()
		doc[r.issuer] = r.entry
		mu.Unlock()
	}

	if err := cache.WriteDirectory(cfg.CacheDir, doc); err != nil {
		return nil, err
	}
	if err := cache.WriteFile(cfg.CacheFile, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func cachedEntry(cfg config.Config, issuer string, existing cache.Document) (cache.Entry, bool, error) {
	if cfg.CacheDir != "" {
		if entry, ok, err := cache.LoadHashed(cfg.CacheDir, issuer); err != nil {
			return cache.Entry{}, false, fmt.Errorf("load cache dir: %w", err)
		} else if ok {
			return entry, true, nil
		}
	}
	if entry, ok := existing[issuer]; ok {
		return entry, true, nil
	}
	return cache.Entry{}, false, nil
}

func fetchIssuerWithRetry(ctx context.Context, client *http.Client, jwksURI string, ttl time.Duration, cached cache.Entry, hasCached bool) (cache.Entry, error) {
	const maxAttempts = 3
	backoff := 200 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		entry, retryable, err := fetchIssuer(ctx, client, jwksURI, ttl, cached, hasCached)
		if err == nil {
			return entry, nil
		}
		lastErr = err
		if !retryable || attempt == maxAttempts-1 {
			break
		}
		select {
		case <-time.After(backoff):
			backoff *= 2
			continue
		case <-ctx.Done():
			return cache.Entry{}, ctx.Err()
		}
	}
	return cache.Entry{}, lastErr
}

func fetchIssuer(ctx context.Context, client *http.Client, jwksURI string, ttl time.Duration, cached cache.Entry, hasCached bool) (cache.Entry, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return cache.Entry{}, false, fmt.Errorf("build request: %w", err)
	}
	if hasCached && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return cache.Entry{}, true, fmt.Errorf("request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotModified {
		if !hasCached {
			return cache.Entry{}, false, errors.New("304 received without cached entry")
		}
		entry := cache.BuildEntry(cached.JWKS, ttl)
		entry.ETag = cached.ETag
		return entry, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return cache.Entry{}, retryable, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var jwks map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return cache.Entry{}, false, fmt.Errorf("decode jwks: %w", err)
	}
	if err := validateJWKS(jwks); err != nil {
		return cache.Entry{}, false, err
	}

	entry := cache.BuildEntry(jwks, ttl)
	entry.ETag = resp.Header.Get("ETag")
	return entry, false, nil
}

func mergeDocs(dst cache.Document, src cache.Document) {
	for k, v := range src {
		dst[k] = v
	}
}

// limiter controls concurrency globally and per-host.
type limiter struct {
	global chan struct{}
	mu     sync.Mutex
	per    map[string]chan struct{}
	limit  int
}

func newLimiter(globalLimit, perHostLimit int) *limiter {
	l := &limiter{limit: perHostLimit, per: make(map[string]chan struct{})}
	if globalLimit > 0 {
		l.global = make(chan struct{}, globalLimit)
	}
	return l
}

func (l *limiter) acquire(host string) func() {
	releaseGlobal := func() {}
	if l.global != nil {
		l.global <- struct{}{}
		releaseGlobal = func() { <-l.global }
	}

	if l.limit <= 0 {
		return releaseGlobal
	}

	l.mu.Lock()
	ch, ok := l.per[host]
	if !ok {
		ch = make(chan struct{}, l.limit)
		l.per[host] = ch
	}
	l.mu.Unlock()

	ch <- struct{}{}
	return func() {
		<-ch
		releaseGlobal()
	}
}

func hostFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host in url: %s", raw)
	}
	return u.Host, nil
}

func resolveJWKSURI(ctx context.Context, client *http.Client, iss config.IssuerConfig) (string, error) {
	if iss.JWKSURI != "" {
		return iss.JWKSURI, nil
	}

	issuerURL, err := normalizeIssuer(iss.Issuer)
	if err != nil {
		return "", err
	}

	oidcURL := oidcWellKnown(issuerURL)
	if uri, err := discover(ctx, client, oidcURL); err == nil && uri != "" {
		return uri, nil
	}

	oauthURL := oauthWellKnown(issuerURL)
	if uri, err := discover(ctx, client, oauthURL); err == nil && uri != "" {
		return uri, nil
	}

	return "", fmt.Errorf("discovery failed for %s", iss.Issuer)
}

func normalizeIssuer(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse issuer: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("issuer missing scheme or host: %s", raw)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}

func oidcWellKnown(u *url.URL) string {
	clone := *u
	clone.Path = path.Join(u.Path, "/.well-known/openid-configuration")
	return clone.String()
}

func oauthWellKnown(u *url.URL) string {
	clone := *u
	clone.Path = path.Join("/.well-known/oauth-authorization-server", u.Path)
	return clone.String()
}

type discoveryDoc struct {
	JWKSURI string `json:"jwks_uri"`
}

func discover(ctx context.Context, client *http.Client, wellKnownURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return "", fmt.Errorf("build discovery request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discovery request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery status %s", resp.Status)
	}

	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decode discovery: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", errors.New("jwks_uri missing in discovery")
	}
	return doc.JWKSURI, nil
}

func validateJWKS(jwks map[string]any) error {
	if len(jwks) == 0 {
		return errors.New("empty jwks response")
	}

	keysVal, ok := jwks["keys"]
	if !ok {
		return errors.New("jwks keys missing")
	}
	keys, ok := keysVal.([]any)
	if !ok {
		return errors.New("jwks keys must be an array")
	}
	for i, v := range keys {
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("jwks keys[%d] must be an object", i)
		}
	}
	return nil
}
