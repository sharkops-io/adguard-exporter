package adguard

import (
	"testing"

	"gitlab.com/sharkops/exporters/exporter-common/metric"
)

func byName(samples []metric.Sample, name string) []metric.Sample {
	var out []metric.Sample
	for _, s := range samples {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func mustOne(t *testing.T, samples []metric.Sample, name string) metric.Sample {
	t.Helper()
	got := byName(samples, name)
	if len(got) != 1 {
		t.Fatalf("%s: got %d samples, want 1", name, len(got))
	}
	return got[0]
}

func testSnapshot() *Snapshot {
	return &Snapshot{
		Status:      Status{Version: "v0.107.78", ProtectionEnabled: true, Running: true},
		StatsConfig: StatsConfig{Interval: 86400000, Enabled: true},
		Stats: Stats{
			TimeUnits:               "hours",
			DNSQueries:              mkBuckets(100, 200, 300),
			BlockedFiltering:        mkBuckets(10, 20, 30),
			NumDNSQueries:           127317,
			NumBlockedFiltering:     13314,
			NumReplacedSafebrowsing: 0,
			NumReplacedSafesearch:   0,
			NumReplacedParental:     0,
			AvgProcessingTime:       0.018173,
			TopClients:              []map[string]float64{{"192.0.2.10": 23282}},
			TopUpstreamsResponses:   []map[string]float64{{"https://dns10.quad9.net:443/dns-query": 66339}},
			TopUpstreamsAvgTime:     []map[string]float64{{"1.1.1.1:53": 0.0298}},
		},
	}
}

// mkBuckets builds a 24-element array whose last three entries are the given
// values, so index 22 (the last COMPLETE hour) is distinguishable from index
// 23 (the current partial one).
func mkBuckets(at21, at22, at23 float64) []float64 {
	b := make([]float64, 24)
	b[21], b[22], b[23] = at21, at22, at23
	return b
}

func TestScalarGauges(t *testing.T) {
	s := Samples(testSnapshot(), "host1")

	for name, want := range map[string]float64{
		"adguard_running":                     1,
		"adguard_protection_enabled":          1,
		"adguard_stats_window_seconds":        86400,
		"adguard_dns_queries_24h":             127317,
		"adguard_blocked_filtered_24h":        13314,
		"adguard_avg_processing_time_seconds": 0.018173,
	} {
		if got := mustOne(t, s, name).Value; got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// Index 22, not 23: index 23 is the current partial hour and would read as a
// collapse in traffic every time the hour rolls over.
func TestLastHourUsesIndex22(t *testing.T) {
	s := Samples(testSnapshot(), "host1")
	if got := mustOne(t, s, "adguard_dns_queries_last_hour").Value; got != 200 {
		t.Errorf("adguard_dns_queries_last_hour = %v, want 200 (index 22)", got)
	}
	if got := mustOne(t, s, "adguard_blocked_filtered_last_hour").Value; got != 20 {
		t.Errorf("adguard_blocked_filtered_last_hour = %v, want 20 (index 22)", got)
	}
}

// When the stats interval exceeds 24h AdGuard returns DAILY buckets, and a
// series named _last_hour would be a lie. Omit rather than mislabel: an
// absent series is visible in Grafana, a wrong one is not.
func TestLastHourOmittedWhenUnitsAreNotHours(t *testing.T) {
	snap := testSnapshot()
	snap.Stats.TimeUnits = "days"
	s := Samples(snap, "host1")
	if got := byName(s, "adguard_dns_queries_last_hour"); len(got) != 0 {
		t.Errorf("emitted %d _last_hour samples with time_units=days, want 0", len(got))
	}
	// The window gauge must still be published so the alert can fire.
	mustOne(t, s, "adguard_stats_window_seconds")
}

func TestLastHourOmittedWhenBucketsAreShort(t *testing.T) {
	snap := testSnapshot()
	snap.Stats.DNSQueries = []float64{1, 2}
	snap.Stats.BlockedFiltering = []float64{1, 2}
	s := Samples(snap, "host1")
	if got := byName(s, "adguard_dns_queries_last_hour"); len(got) != 0 {
		t.Errorf("emitted _last_hour from a 2-element bucket array, want 0")
	}
}

func TestBuildInfoCarriesVersion(t *testing.T) {
	s := mustOne(t, Samples(testSnapshot(), "host1"), "adguard_build_info")
	if s.Labels["version"] != "v0.107.78" {
		t.Errorf("version label = %q, want v0.107.78", s.Labels["version"])
	}
	if s.Value != 1 {
		t.Errorf("build_info value = %v, want 1", s.Value)
	}
}

func TestTopNLabels(t *testing.T) {
	s := Samples(testSnapshot(), "host1")

	c := mustOne(t, s, "adguard_top_clients")
	if c.Labels["client"] != "192.0.2.10" || c.Value != 23282 {
		t.Errorf("top_clients = %+v", c)
	}
	u := mustOne(t, s, "adguard_top_upstreams_responses")
	if u.Labels["upstream"] != "https://dns10.quad9.net:443/dns-query" || u.Value != 66339 {
		t.Errorf("top_upstreams_responses = %+v", u)
	}
	a := mustOne(t, s, "adguard_top_upstreams_avg_time_seconds")
	if a.Labels["upstream"] != "1.1.1.1:53" || a.Value != 0.0298 {
		t.Errorf("top_upstreams_avg_time_seconds = %+v", a)
	}
}

// Every sample this exporter emits must carry `server`, not just the two
// self-monitoring gauges collector.NewWithLabels reaches. Without it, two
// AdGuard instances' otherwise name-and-label-identical device series
// collide at Gather and one instance's whole payload is silently dropped.
func TestServerLabelOnEverySample(t *testing.T) {
	for _, s := range Samples(testSnapshot(), "host1") {
		if got := s.Labels["server"]; got != "host1" {
			t.Errorf("sample %s: server label = %q, want %q", s.Name, got, "host1")
		}
	}
}

// Domain series (top_queried_domains, top_blocked_domains) are deliberately
// absent: they are 100 entries per server whose label values churn
// constantly, and excluding them is the whole reason this exporter exists
// rather than the third-party one, whose /metrics endpoint deadlocks from
// exactly that growth.
//
// Asserting "no domain label" directly is vacuous here: Stats has no domain
// fields and testSnapshot supplies none, so that assertion could never fail
// regardless of what Samples does. Asserting the exact metric NAME SET
// instead catches a domain series being added back in, AND catches any other
// unintended metric appearing or disappearing.
func TestEmittedMetricNameSetIsExact(t *testing.T) {
	want := map[string]bool{
		"adguard_running":                        true,
		"adguard_protection_enabled":             true,
		"adguard_build_info":                     true,
		"adguard_stats_window_seconds":           true,
		"adguard_dns_queries_24h":                true,
		"adguard_blocked_filtered_24h":           true,
		"adguard_replaced_safebrowsing_24h":      true,
		"adguard_replaced_safesearch_24h":        true,
		"adguard_replaced_parental_24h":          true,
		"adguard_dns_queries_last_hour":          true,
		"adguard_blocked_filtered_last_hour":     true,
		"adguard_avg_processing_time_seconds":    true,
		"adguard_top_clients":                    true,
		"adguard_top_upstreams_responses":        true,
		"adguard_top_upstreams_avg_time_seconds": true,
	}

	got := map[string]bool{}
	for _, s := range Samples(testSnapshot(), "host1") {
		got[s.Name] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("missing expected metric %s", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected metric %s (e.g. a reintroduced domain series)", name)
		}
	}
}

// The whole design rests on every sample being a GAUGE: AdGuard's Num*
// fields are totals over the statistics window that DECREASE as buckets age
// out, not lifetime counters. An accidentally-Counter'd sample would produce
// silent nonsense under rate()/increase() that no other test here catches.
func TestAllSamplesAreGauges(t *testing.T) {
	for _, s := range Samples(testSnapshot(), "host1") {
		if s.Kind != metric.KindGauge {
			t.Errorf("sample %s has Kind %v, want KindGauge", s.Name, s.Kind)
		}
	}
}

func TestFalseBooleansArePublishedAsZero(t *testing.T) {
	snap := testSnapshot()
	snap.Status.Running = false
	snap.Status.ProtectionEnabled = false
	s := Samples(snap, "host1")
	if got := mustOne(t, s, "adguard_running").Value; got != 0 {
		t.Errorf("adguard_running = %v, want 0", got)
	}
	if got := mustOne(t, s, "adguard_protection_enabled").Value; got != 0 {
		t.Errorf("adguard_protection_enabled = %v, want 0", got)
	}
}
