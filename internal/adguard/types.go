package adguard

// Response shapes for the AdGuard Home v0.107.x control API. Only the fields
// this exporter publishes are modelled.
//
// The top-N lists are arrays of SINGLE-KEY objects, not objects — AdGuard
// returns [{"192.0.2.10": 23282}, ...] — hence []map[string]float64.
type Status struct {
	Version           string `json:"version"`
	ProtectionEnabled bool   `json:"protection_enabled"`
	Running           bool   `json:"running"`
}

type Stats struct {
	// TimeUnits is "hours" or "days". The per-bucket metrics are only
	// meaningful when it is "hours"; see samples.go.
	TimeUnits string `json:"time_units"`

	// Hourly buckets, oldest first. Index 23 is the CURRENT PARTIAL hour, so
	// index 22 is the last complete one.
	DNSQueries       []float64 `json:"dns_queries"`
	BlockedFiltering []float64 `json:"blocked_filtering"`

	// Num* are totals over the statistics window (24h by default), NOT
	// lifetime counters. They decrease as buckets age out, so every metric
	// derived from them is a gauge.
	NumDNSQueries           float64 `json:"num_dns_queries"`
	NumBlockedFiltering     float64 `json:"num_blocked_filtering"`
	NumReplacedSafebrowsing float64 `json:"num_replaced_safebrowsing"`
	NumReplacedSafesearch   float64 `json:"num_replaced_safesearch"`
	NumReplacedParental     float64 `json:"num_replaced_parental"`

	// AvgProcessingTime is in SECONDS, despite upstream exporters naming it
	// milliseconds. Verified against the query log's elapsedMs: the ratio is
	// ~1000. Do not scale it.
	AvgProcessingTime float64 `json:"avg_processing_time"`

	TopClients            []map[string]float64 `json:"top_clients"`
	TopUpstreamsResponses []map[string]float64 `json:"top_upstreams_responses"`
	// Also already in seconds.
	TopUpstreamsAvgTime []map[string]float64 `json:"top_upstreams_avg_time"`
}

// StatsConfig carries the statistics window. Interval is in MILLISECONDS.
type StatsConfig struct {
	Interval int64 `json:"interval"`
	Enabled  bool  `json:"enabled"`
}

// Snapshot is one collection's worth of payloads.
type Snapshot struct {
	Status      Status
	Stats       Stats
	StatsConfig StatsConfig
}
