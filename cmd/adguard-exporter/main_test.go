package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sharkops-io/adguard-exporter/internal/config"
)

func fakeAdGuard(t *testing.T) *httptest.Server {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile("../../internal/adguard/testdata/" + name + ".json")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		return b
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/control/status", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(read("status")) })
	mux.HandleFunc("/control/stats/config", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(read("stats_config")) })
	mux.HandleFunc("/control/stats", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(read("stats")) })
	return httptest.NewServer(mux)
}

// Two collectors share the namespace "adguard". Registered with
// collector.New, their _scrape_collector_success gauges are label-identical;
// because the Collector is unchecked, the registry does not reject that at
// Register time — it fails the entire Gather, taking every other metric on
// the endpoint with it. NewWithLabels is what prevents that, and this test is
// what stops a future edit from quietly reverting to New.
func TestTwoServersGatherWithoutCollision(t *testing.T) {
	a := fakeAdGuard(t)
	defer a.Close()
	b := fakeAdGuard(t)
	defer b.Close()

	cfg, err := config.Parse(nil, func(k string) string {
		switch k {
		case "ADGUARD_SERVERS":
			return a.URL + "," + b.URL
		case "ADGUARD_NAMES":
			return "host1,pi"
		case "ADGUARD_USERNAMES":
			return "u,u"
		case "ADGUARD_PASSWORDS":
			return "p,p"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}

	reg, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}

	rec := httptest.NewRecorder()
	metricsHandler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// Both servers must be present and distinguishable.
	for _, want := range []string{
		`adguard_scrape_collector_success{server="host1"} 1`,
		`adguard_scrape_collector_success{server="pi"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// NOTE: promhttp_metric_handler_errors_total{cause="gathering"} is NOT a
	// reliable collision check — see TestTwoServersDeviceMetricsDoNotCollide
	// below for why, and for the check that actually caught the bug this
	// test's namesake was written to guard against (device samples with no
	// server label collided across instances; only the two self-monitoring
	// gauges checked above were ever actually disambiguated). That counter
	// lags one scrape behind its own Gather call, so a test doing exactly
	// one HTTP scrape, like this one, would read 0 even when the Gather that
	// produced this very response body failed with a MultiError — asserting
	// on it here would silently guard nothing.
	if strings.Contains(body, "adguard_top_queried_domains") {
		t.Error("domain series present; they are deliberately excluded")
	}
}

// TestTwoServersDeviceMetricsDoNotCollide is the regression test for the bug
// TestTwoServersGatherWithoutCollision missed: device samples (as opposed to
// the two self-monitoring gauges) carried no `server` label at all, so with
// two instances configured, EVERY device series — adguard_dns_queries_24h
// included — was name-and-label-identical across them. prometheus.Registry
// silently drops the second occurrence and records the failure only on
// promhttp_metric_handler_errors_total{cause="gathering"} — a counter that
// itself lags one scrape behind (promhttp gathers its own current value
// before incrementing it for the Gather that just failed), so a test doing
// exactly one HTTP scrape, like TestTwoServersGatherWithoutCollision, reads
// 0 even when that very Gather call produced a MultiError.
//
// Calling reg.Gather() directly sidesteps the lag entirely: the returned
// error is the honest, un-lagged signal.
func TestTwoServersDeviceMetricsDoNotCollide(t *testing.T) {
	a := fakeAdGuard(t)
	defer a.Close()
	b := fakeAdGuard(t)
	defer b.Close()

	cfg, err := config.Parse(nil, func(k string) string {
		switch k {
		case "ADGUARD_SERVERS":
			return a.URL + "," + b.URL
		case "ADGUARD_NAMES":
			return "host1,pi"
		case "ADGUARD_USERNAMES":
			return "u,u"
		case "ADGUARD_PASSWORDS":
			return "p,p"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}

	reg, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}

	mfs, gerr := reg.Gather()
	if gerr != nil {
		t.Fatalf("Gather returned an error — device samples collided across servers: %v", gerr)
	}

	servers := map[string]bool{}
	for _, mf := range mfs {
		if mf.GetName() != "adguard_dns_queries_24h" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "server" {
					servers[lp.GetValue()] = true
				}
			}
		}
	}
	for _, want := range []string{"host1", "pi"} {
		if !servers[want] {
			t.Errorf("adguard_dns_queries_24h missing server=%q; got servers %v", want, servers)
		}
	}
}

func TestNoServersFailsLoudly(t *testing.T) {
	if _, err := config.Parse(nil, func(string) string { return "" }); err == nil {
		t.Fatal("an empty configuration was accepted")
	}
}
