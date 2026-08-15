package adguard

import (
	"context"

	"gitlab.com/sharkops/exporters/exporter-common/metric"
)

// Namespace is the metric prefix and also prefixes the collector's own
// self-monitoring series.
const Namespace = "adguard"

// lastCompleteHour is the index of the last COMPLETE hourly bucket. AdGuard
// returns 24 buckets oldest-first, and index 23 is the hour in progress —
// reading it would show traffic collapsing every time the hour rolls over.
const lastCompleteHour = 22

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// withServer merges the server label into extra (which may be nil). extra is
// never mutated; a fresh map is always returned so callers may safely reuse
// literal maps across calls.
func withServer(server string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	out["server"] = server
	return out
}

// Samples converts one snapshot into exposition.
//
// Every series here is a GAUGE. AdGuard's Num* fields are totals over the
// statistics window, not lifetime counters: they decrease as buckets age out.
// Naming any of them _total, or applying rate(), produces silent nonsense.
//
// server becomes the `server` label on every returned sample. This is the
// mechanism that disambiguates two AdGuard instances' device data on a shared
// registry — collector.NewWithLabels (see cmd/adguard-exporter) only labels
// the two self-monitoring gauges, which carry no Sample of their own. Without
// this, every scalar and topN series here would be a name-and-label-identical
// duplicate across instances, and prometheus.Registry.Gather would silently
// drop the second instance's entire payload.
func Samples(snap *Snapshot, server string) []metric.Sample {
	st := snap.Stats
	out := []metric.Sample{
		metric.Gauge("adguard_running",
			"Whether AdGuard Home reports itself as running (1) or not (0).",
			b2f(snap.Status.Running), withServer(server, nil)),
		metric.Gauge("adguard_protection_enabled",
			"Whether DNS filtering is enabled (1) or disabled (0).",
			b2f(snap.Status.ProtectionEnabled), withServer(server, nil)),
		metric.Gauge("adguard_build_info",
			"AdGuard Home build information. Always 1; read the version label.",
			1, withServer(server, map[string]string{"version": snap.Status.Version})),
		metric.Gauge("adguard_stats_window_seconds",
			"Width of the AdGuard statistics window. The _24h metrics are only correctly named while this is 86400.",
			float64(snap.StatsConfig.Interval)/1000, withServer(server, nil)),

		metric.Gauge("adguard_dns_queries_24h",
			"DNS queries handled during the statistics window. A rolling window, not a counter.",
			st.NumDNSQueries, withServer(server, nil)),
		metric.Gauge("adguard_blocked_filtered_24h",
			"Queries blocked by filter lists during the statistics window.",
			st.NumBlockedFiltering, withServer(server, nil)),
		metric.Gauge("adguard_replaced_safebrowsing_24h",
			"Queries replaced by safe browsing during the statistics window.",
			st.NumReplacedSafebrowsing, withServer(server, nil)),
		metric.Gauge("adguard_replaced_safesearch_24h",
			"Queries replaced by safe search during the statistics window.",
			st.NumReplacedSafesearch, withServer(server, nil)),
		metric.Gauge("adguard_replaced_parental_24h",
			"Queries replaced by parental filtering during the statistics window.",
			st.NumReplacedParental, withServer(server, nil)),
		metric.Gauge("adguard_avg_processing_time_seconds",
			"Mean query processing time over the statistics window, in seconds.",
			st.AvgProcessingTime, withServer(server, nil)),
	}

	// Only meaningful with hourly buckets. Above a 24h stats interval AdGuard
	// switches to daily ones, and a series named _last_hour would be a lie —
	// so omit it. An absent series is visible in Grafana; a mislabelled one
	// is not.
	if st.TimeUnits == "hours" {
		if len(st.DNSQueries) > lastCompleteHour {
			out = append(out, metric.Gauge("adguard_dns_queries_last_hour",
				"DNS queries during the last complete hour.",
				st.DNSQueries[lastCompleteHour], withServer(server, nil)))
		}
		if len(st.BlockedFiltering) > lastCompleteHour {
			out = append(out, metric.Gauge("adguard_blocked_filtered_last_hour",
				"Queries blocked by filter lists during the last complete hour.",
				st.BlockedFiltering[lastCompleteHour], withServer(server, nil)))
		}
	}

	out = append(out, topN(st.TopClients, "adguard_top_clients", "client",
		"Queries per client during the statistics window.", server)...)
	out = append(out, topN(st.TopUpstreamsResponses, "adguard_top_upstreams_responses", "upstream",
		"Responses per upstream resolver during the statistics window.", server)...)
	out = append(out, topN(st.TopUpstreamsAvgTime, "adguard_top_upstreams_avg_time_seconds", "upstream",
		"Mean response time per upstream resolver, in seconds.", server)...)

	return out
}

// topN flattens AdGuard's array-of-single-key-objects into samples.
//
// Domain lists are deliberately NOT passed here. top_queried_domains and
// top_blocked_domains are 100 entries per server whose label values churn
// constantly; excluding them is why this exporter exists rather than the
// third-party one, whose /metrics endpoint deadlocks from exactly that growth.
func topN(entries []map[string]float64, name, label, help, server string) []metric.Sample {
	out := make([]metric.Sample, 0, len(entries))
	for _, e := range entries {
		for k, v := range e {
			out = append(out, metric.Gauge(name, help, v, withServer(server, map[string]string{label: k})))
		}
	}
	return out
}

// Source adapts a Client to collector.Source. Fields are unexported so
// NewSource is the only way to construct one.
type Source struct {
	client *Client
	// name is the server label stamped onto every device sample. See Config.Name.
	name string
}

// NewSource builds a Source. The name is a required constructor argument
// rather than a settable field: an empty server label collides every device
// series across instances, and the compiler is the only reliable place to
// catch it — which it now does, since Source's fields are unexported and
// this is the only constructor.
func NewSource(client *Client, name string) *Source {
	return &Source{client: client, name: name}
}

func (s *Source) Namespace() string { return Namespace }

func (s *Source) Collect(ctx context.Context) ([]metric.Sample, error) {
	snap, err := s.client.GetSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return Samples(snap, s.name), nil
}
