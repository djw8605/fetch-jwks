package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/bbockelm/fetch-jwks/internal/config"
	"github.com/bbockelm/fetch-jwks/internal/fetcher"
)

type issuerFlag []config.IssuerConfig

func (i *issuerFlag) String() string {
	return ""
}

// Set expects issuer and jwks_uri separated by a comma, e.g.:
// -issuer "issuer=https://demo.scitokens.org,jwks_uri=https://demo.scitokens.org/jwks"
func (i *issuerFlag) Set(value string) error {
	var iss config.IssuerConfig
	parts := parseKeyValues(value)
	iss.Issuer = parts["issuer"]
	iss.JWKSURI = parts["jwks_uri"]
	if iss.Issuer == "" {
		return errors.New("issuer flag requires issuer")
	}
	*i = append(*i, iss)
	return nil
}

func main() {
	var (
		configFile = flag.String("config", "/etc/fetch-jwks.conf", "Path to YAML config file")
		configDir  = flag.String("config-dir", "/etc/fetch-jwks.config.d", "Directory of YAML config fragments")
		outDir     = flag.String("out-dir", "", "Directory to write hashed cache files (overrides config)")
		outFile    = flag.String("out-file", "", "File to write aggregated cache JSON (overrides config)")
		ttlFlag    = flag.Duration("ttl", 0, "Override cache TTL (e.g. 6h)")
		timeout    = flag.Duration("timeout", 0, "HTTP request timeout (e.g. 10s)")
		maxPar     = flag.Int("max-parallel", 0, "Maximum concurrent fetches (0 or negative for unlimited)")
		maxHost    = flag.Int("max-per-host", 0, "Maximum concurrent fetches per host (0 or negative for unlimited)")
	)
	var issuers issuerFlag
	flag.Var(&issuers, "issuer", "Issuer specification issuer=<url>,jwks_uri=<url>; repeatable")
	flag.Parse()

	cfg, err := config.Load(*configFile, *configDir)
	if err != nil {
		fatalf("load config: %v", err)
	}

	if len(issuers) > 0 {
		cfg.Issuers = append(cfg.Issuers, issuers...)
	}

	if *outDir != "" {
		cfg.CacheDir = *outDir
	}
	if *outFile != "" {
		cfg.CacheFile = *outFile
	}
	if *ttlFlag != 0 {
		cfg.TTL = config.Duration{Duration: *ttlFlag}
	}
	if *timeout != 0 {
		cfg.RequestTimeout = config.Duration{Duration: *timeout}
	}
	if *maxPar != 0 {
		cfg.MaxParallel = *maxPar
	}
	if *maxHost != 0 {
		cfg.MaxPerHost = *maxHost
	}

	client := &http.Client{Timeout: cfg.RequestTimeout.Duration}
	doc, err := fetcher.Run(context.Background(), client, cfg)
	if err != nil {
		fatalf("%v", err)
	}
	for iss := range doc {
		fmt.Printf("fetched %s\n", iss)
	}
	fmt.Println("done")
}

func parseKeyValues(s string) map[string]string {
	parts := make(map[string]string)
	fields := splitAndTrim(s, ',')
	for _, f := range fields {
		kv := splitAndTrim(f, '=')
		if len(kv) != 2 {
			continue
		}
		parts[kv[0]] = kv[1]
	}
	return parts
}

func splitAndTrim(s string, sep rune) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == sep {
			if start <= i {
				out = append(out, trimSpaces(s[start:i]))
			}
			start = i + 1
		}
	}
	if start <= len(s) {
		out = append(out, trimSpaces(s[start:]))
	}
	return out
}

func trimSpaces(s string) string {
	for len(s) > 0 {
		if s[0] == ' ' || s[0] == '\t' {
			s = s[1:]
			continue
		}
		if s[len(s)-1] == ' ' || s[len(s)-1] == '\t' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fetch-jwks: "+format+"\n", args...)
	os.Exit(1)
}
