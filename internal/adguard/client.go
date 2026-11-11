package adguard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sharkops-io/exporter-common/httpx"
)

// Config is one AdGuard Home instance's connection config.
type Config struct {
	// Name becomes the `server` label. Two mechanisms apply it, and both are
	// required: NewSource's name argument threads it into every DEVICE sample (Samples), and
	// collector.NewWithLabels threads it into the two self-monitoring gauges
	// (_scrape_collector_success/_duration_seconds), which carry no Sample of
	// their own for it to land on. It MUST be unique across instances — see
	// cmd/adguard-exporter.
	Name     string
	URL      string
	Username string
	Password string

	InsecureSkipVerify bool
	// Timeout bounds each individual HTTP request. GetSnapshot makes THREE
	// sequential requests, so 3x this value must fit inside the collector
	// timeout (--collector.timeout), which must itself fit inside
	// Prometheus' scrape_timeout. With the shipped defaults (10s here,
	// 25s collector timeout), a server that takes more than ~8s per
	// request can exhaust the collector timeout. If a target is slow,
	// lower this value rather than raising --collector.timeout above
	// scrape_timeout.
	Timeout time.Duration
}

const (
	statusPath      = "/control/status"
	statsPath       = "/control/stats"
	statsConfigPath = "/control/stats/config"
)

type Client struct {
	http    *http.Client
	baseURL string
	headers map[string]string
}

// New builds a Client. AdGuard's control API accepts HTTP basic auth, so no
// session handshake is needed.
func New(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, errors.New("url is empty")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, errors.New("username and password are required")
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
	return &Client{
		http: httpx.New(httpx.Options{
			Timeout:            cfg.Timeout,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}),
		baseURL: strings.TrimSuffix(cfg.URL, "/"),
		headers: map[string]string{"Authorization": "Basic " + auth},
	}, nil
}

// GetSnapshot fetches all three payloads. Any failure fails the whole
// collection: a partial snapshot would publish some series and silently drop
// others, which reads in Grafana as a healthy resolver with missing panels.
func (c *Client) GetSnapshot(ctx context.Context) (*Snapshot, error) {
	var snap Snapshot
	for _, step := range []struct {
		path string
		dst  any
	}{
		{statusPath, &snap.Status},
		{statsPath, &snap.Stats},
		{statsConfigPath, &snap.StatsConfig},
	} {
		body, err := httpx.GetBytes(ctx, c.http, c.baseURL+step.path, c.headers)
		if err != nil {
			return nil, fmt.Errorf("get %s: %w", step.path, err)
		}
		if err := json.Unmarshal(body, step.dst); err != nil {
			return nil, fmt.Errorf("decode %s: %w", step.path, err)
		}
	}
	return &snap, nil
}
