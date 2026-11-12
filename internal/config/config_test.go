package config_test

import (
	"errors"
	"testing"

	"github.com/sharkops-io/adguard-exporter/internal/config"
)

func noEnv(string) string { return "" }

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParsesServersFromEnv(t *testing.T) {
	cfg, err := config.Parse(nil, envFrom(map[string]string{
		"ADGUARD_SERVERS":   "http://a:3000,http://b",
		"ADGUARD_NAMES":     "dns1,dns2",
		"ADGUARD_USERNAMES": "ua,ub",
		"ADGUARD_PASSWORDS": "pa,pb",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("len(Servers) = %d, want 2", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "dns1" || cfg.Servers[0].URL != "http://a:3000" {
		t.Errorf("Servers[0] = %+v", cfg.Servers[0])
	}
	if cfg.Servers[1].Username != "ub" || cfg.Servers[1].Password != "pb" {
		t.Errorf("Servers[1] credentials wrong: %+v", cfg.Servers[1])
	}
}

// Names default to the URL so a misconfigured deployment still produces
// distinguishable series rather than colliding ones.
func TestNamesDefaultToURL(t *testing.T) {
	cfg, err := config.Parse(nil, envFrom(map[string]string{
		"ADGUARD_SERVERS":   "http://a:3000",
		"ADGUARD_USERNAMES": "u",
		"ADGUARD_PASSWORDS": "p",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Servers[0].Name != "http://a:3000" {
		t.Errorf("Name = %q, want the URL", cfg.Servers[0].Name)
	}
}

// A duplicate name would give two collectors label-identical
// _scrape_collector_success gauges, which fails the whole Gather rather than
// being caught at registration. Reject it at config time instead.
func TestDuplicateNamesRejected(t *testing.T) {
	_, err := config.Parse(nil, envFrom(map[string]string{
		"ADGUARD_SERVERS":   "http://a,http://b",
		"ADGUARD_NAMES":     "same,same",
		"ADGUARD_USERNAMES": "ua,ub",
		"ADGUARD_PASSWORDS": "pa,pb",
	}))
	if err == nil {
		t.Fatal("Parse accepted duplicate server names")
	}
}

func TestMismatchedListLengthsRejected(t *testing.T) {
	_, err := config.Parse(nil, envFrom(map[string]string{
		"ADGUARD_SERVERS":   "http://a,http://b",
		"ADGUARD_USERNAMES": "only-one",
		"ADGUARD_PASSWORDS": "pa,pb",
	}))
	if err == nil {
		t.Fatal("Parse accepted mismatched list lengths")
	}
}

// An exporter with nothing configured must fail loudly rather than serve an
// empty /metrics forever.
func TestNoServersIsAnError(t *testing.T) {
	if _, err := config.Parse(nil, noEnv); err == nil {
		t.Fatal("Parse accepted an empty configuration")
	}
}

func TestHelpIsItsOwnSignal(t *testing.T) {
	_, err := config.Parse([]string{"--help"}, noEnv)
	if !errors.Is(err, config.ErrHelpRequested) {
		t.Fatalf("Parse(--help) = %v, want ErrHelpRequested", err)
	}
}

func TestFlagsBeatEnvironment(t *testing.T) {
	cfg, err := config.Parse(
		[]string{"--adguard.servers=http://flag", "--adguard.usernames=fu", "--adguard.passwords=fp"},
		envFrom(map[string]string{
			"ADGUARD_SERVERS":   "http://env",
			"ADGUARD_USERNAMES": "eu",
			"ADGUARD_PASSWORDS": "ep",
		}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Servers[0].URL != "http://flag" {
		t.Errorf("URL = %q, want the flag value", cfg.Servers[0].URL)
	}
}
