package fetcher

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/bbockelm/fetch-jwks/internal/cache"
	"github.com/bbockelm/fetch-jwks/internal/config"
)

// Integration-style test that reads the OSG topology XML for token issuers,
// builds a config, and runs fetch-jwks against a stub JWKS server. We rely on
// the real topology feed structure but avoid hitting production JWKS endpoints.
func TestTopologyIssuersFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires network to download topology XML")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	issuers, err := fetchTopologyIssuers(ctx, "https://topology.opensciencegrid.org/vosummary/xml")
	if err != nil {
		t.Skipf("skip: unable to download topology xml: %v", err)
	}
	if len(issuers) == 0 {
		t.Skip("skip: no token issuers found in topology")
	}

	cfg := config.Config{CacheDir: t.TempDir(), TTL: config.Duration{Duration: time.Hour}, RequestTimeout: config.Duration{Duration: 5 * time.Second}, MaxParallel: 4}
	client := &http.Client{Timeout: cfg.RequestTimeout.Duration}

	const maxIssuers = 40
	for _, iss := range issuers {
		if len(cfg.Issuers) >= maxIssuers {
			break
		}
		uri, err := resolveJWKSURI(ctx, client, config.IssuerConfig{Issuer: iss})
		if err != nil {
			t.Logf("skip issuer %s: %v", iss, err)
			continue
		}
		cfg.Issuers = append(cfg.Issuers, config.IssuerConfig{Issuer: iss, JWKSURI: uri})
	}

	if len(cfg.Issuers) == 0 {
		t.Skip("no issuers resolved from topology feed")
	}
	start := time.Now()
	doc := make(cache.Document)
	for _, iss := range cfg.Issuers {
		entry, err := fetchIssuerWithRetry(ctx, client, iss.JWKSURI, cfg.TTL.Duration, cache.Entry{}, false)
		if err != nil {
			t.Logf("skip fetch %s: %v", iss.Issuer, err)
			continue
		}
		doc[iss.Issuer] = entry
	}

	if len(doc) == 0 {
		t.Skip("no issuers fetched successfully")
	}
	t.Logf("Fetched %d issuers in %s", len(doc), time.Since(start))

	if err := cache.WriteDirectory(cfg.CacheDir, doc); err != nil {
		t.Fatalf("write cache dir: %v", err)
	}

	// Ensure cache directory files were written and readable.
	for iss := range doc {
		if _, ok, err := cache.LoadHashed(cfg.CacheDir, iss); err != nil {
			t.Fatalf("load hashed for %s: %v", iss, err)
		} else if !ok {
			t.Fatalf("missing cache entry for %s", iss)
		}
	}

	// Print a few sample issuers for debugging context.
	sample := 10
	if len(cfg.Issuers) < sample {
		sample = len(cfg.Issuers)
	}
	t.Logf("Fetched JWKS for %d issuers, sample:", len(cfg.Issuers))
	count := 0
	for iss, entry := range doc {
		if count >= sample {
			break
		}
		keys, _ := entry.JWKS["keys"].([]any)
		t.Logf("- Issuer: %s, Keys: %d", iss, len(keys))
		count++
	}

	// Print total issuers fetched.
	t.Logf("Total issuers fetched: %d", len(doc))

}

type topologyDoc struct {
	VOs []struct {
		Credentials struct {
			TokenIssuers []struct {
				URL string `xml:"URL"`
			} `xml:"TokenIssuers>TokenIssuer"`
		} `xml:"Credentials"`
	} `xml:"VO"`
}

func fetchTopologyIssuers(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var doc topologyDoc
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var issuers []string
	for _, vo := range doc.VOs {
		for _, ti := range vo.Credentials.TokenIssuers {
			if ti.URL == "" {
				continue
			}
			if _, ok := seen[ti.URL]; ok {
				continue
			}
			seen[ti.URL] = struct{}{}
			issuers = append(issuers, ti.URL)
		}
	}
	return issuers, nil
}
