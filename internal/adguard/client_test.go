package adguard

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func newTestServer(t *testing.T, wantUser, wantPass string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(wantUser+":"+wantPass))
			if r.Header.Get("Authorization") != want {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, name))
		}
	}
	mux.HandleFunc("/control/status", serve("status"))
	mux.HandleFunc("/control/stats/config", serve("stats_config"))
	mux.HandleFunc("/control/stats", serve("stats"))
	return httptest.NewServer(mux)
}

func TestGetSnapshotParsesAllThreeEndpoints(t *testing.T) {
	srv := newTestServer(t, "user", "pass")
	defer srv.Close()

	c, err := New(Config{URL: srv.URL, Username: "user", Password: "pass"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap, err := c.GetSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}

	if snap.Status.Version == "" {
		t.Error("Status.Version is empty")
	}
	if !snap.Status.Running {
		t.Error("Status.Running is false; fixture has it true")
	}
	if snap.Stats.NumDNSQueries == 0 {
		t.Error("Stats.NumDNSQueries is zero")
	}
	if snap.Stats.TimeUnits != "hours" {
		t.Errorf("Stats.TimeUnits = %q, want hours", snap.Stats.TimeUnits)
	}
	if len(snap.Stats.DNSQueries) != 24 {
		t.Errorf("len(DNSQueries) = %d, want 24", len(snap.Stats.DNSQueries))
	}
	if snap.StatsConfig.Interval != 86400000 {
		t.Errorf("StatsConfig.Interval = %d, want 86400000", snap.StatsConfig.Interval)
	}
	if len(snap.Stats.TopClients) == 0 {
		t.Error("TopClients is empty")
	}
}

func TestGetSnapshotRejectsBadCredentials(t *testing.T) {
	srv := newTestServer(t, "user", "pass")
	defer srv.Close()

	c, err := New(Config{URL: srv.URL, Username: "user", Password: "wrong"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetSnapshot(context.Background()); err == nil {
		t.Fatal("GetSnapshot accepted bad credentials")
	}
}

func TestNewRejectsEmptyURL(t *testing.T) {
	if _, err := New(Config{Username: "u", Password: "p"}); err == nil {
		t.Fatal("New accepted an empty URL")
	}
}
