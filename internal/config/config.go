// Package config binds this exporter's flags. The flags every exporter in
// this family shares live in exporter-common/config, which this embeds.
package config

import (
	"fmt"
	"strings"

	"github.com/alecthomas/kingpin/v2"

	common "gitlab.com/sharkops/exporters/exporter-common/config"

	"gitlab.com/sharkops/exporters/adguard-exporter/internal/adguard"
)

// ErrHelpRequested is re-exported so main need not import the shared package
// just to check one sentinel.
var ErrHelpRequested = common.ErrHelpRequested

type Config struct {
	*common.Base

	// Servers is the set of AdGuard instances to scrape. Unlike
	// unas-protect-exporter's fixed pair of collectors, this is an N-instance
	// list — which is why the device half of config was not generalised into
	// exporter-common.
	Servers []adguard.Config
}

// Parse builds a Config from args and the environment. getenv is injected so
// the precedence rules are testable without touching the real environment.
func Parse(args []string, getenv func(string) string) (*Config, error) {
	app := kingpin.New("adguard-exporter", "Prometheus exporter for AdGuard Home.")
	base := common.Register(app, common.Defaults{ListenAddress: ":9618"})

	var urls, names, usernames, passwords string
	var insecure bool
	app.Flag("adguard.servers", "Comma-separated AdGuard Home base URLs.").StringVar(&urls)
	app.Flag("adguard.names", "Comma-separated server labels, positionally matching --adguard.servers. Defaults to the URL.").StringVar(&names)
	app.Flag("adguard.usernames", "Comma-separated usernames, positionally matching --adguard.servers.").StringVar(&usernames)
	app.Flag("adguard.passwords", "Comma-separated passwords, positionally matching --adguard.servers.").StringVar(&passwords)
	app.Flag("adguard.insecure-skip-verify", "Disable TLS verification for all servers.").BoolVar(&insecure)

	if err := common.Parse(app, base, args); err != nil {
		return nil, err
	}

	common.Fallback(&urls, getenv("ADGUARD_SERVERS"))
	common.Fallback(&names, getenv("ADGUARD_NAMES"))
	common.Fallback(&usernames, getenv("ADGUARD_USERNAMES"))
	common.Fallback(&passwords, getenv("ADGUARD_PASSWORDS"))

	u := splitList(urls)
	n := splitList(names)
	un := splitList(usernames)
	pw := splitList(passwords)

	if len(u) == 0 {
		return nil, fmt.Errorf("no servers configured; set --adguard.servers or ADGUARD_SERVERS")
	}
	if len(un) != len(u) || len(pw) != len(u) {
		return nil, fmt.Errorf("servers, usernames and passwords must have equal length (got %d, %d, %d)",
			len(u), len(un), len(pw))
	}
	if len(n) != 0 && len(n) != len(u) {
		return nil, fmt.Errorf("names must be empty or match servers in length (got %d, want %d)", len(n), len(u))
	}

	cfg := Config{Base: base}
	seen := make(map[string]bool, len(u))
	for i, url := range u {
		name := url
		if len(n) != 0 {
			name = n[i]
		}
		// A duplicate name gives two collectors label-identical
		// _scrape_collector_success gauges. Because collector.Collector is
		// unchecked, that is NOT caught at Register time — it fails the whole
		// Gather, taking every metric on the endpoint down. Reject it here,
		// where the error is legible.
		if seen[name] {
			return nil, fmt.Errorf("duplicate server name %q", name)
		}
		seen[name] = true

		cfg.Servers = append(cfg.Servers, adguard.Config{
			Name:               name,
			URL:                url,
			Username:           un[i],
			Password:           pw[i],
			InsecureSkipVerify: insecure,
			Timeout:            base.HTTPTimeout,
		})
	}
	return &cfg, nil
}

// splitList splits a comma-separated list, trimming whitespace and dropping
// empty entries so a trailing comma is harmless.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
