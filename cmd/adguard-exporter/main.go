// Command adguard-exporter serves Prometheus metrics for one or more AdGuard
// Home instances.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gitlab.com/sharkops/exporters/exporter-common/collector"

	"gitlab.com/sharkops/exporters/adguard-exporter/internal/adguard"
	"gitlab.com/sharkops/exporters/adguard-exporter/internal/config"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if errors.Is(err, config.ErrHelpRequested) {
		// kingpin already wrote the usage text; --help is success, not a
		// configuration error, so this exits 0 like any conventional CLI.
		os.Exit(0)
	}
	if err != nil {
		// --log.level/--log.format live in the very config that failed to
		// parse, so this message necessarily goes out on the default handler.
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(cfg.LogHandler(os.Stderr)))

	slog.Info("starting adguard-exporter", "version", version, "servers", len(cfg.Servers))

	reg, err := buildRegistry(cfg)
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.TelemetryPath, metricsHandler(reg))
	// Deliberately NO /healthz. A probe that collected would couple this pod's
	// restarts to AdGuard being reachable; a broken exporter should surface as
	// a DOWN Prometheus target instead.
	slog.Info("listening", "address", cfg.ListenAddress, "path", cfg.TelemetryPath)
	if err := http.ListenAndServe(cfg.ListenAddress, mux); err != nil {
		slog.Error("http server", "err", err)
		os.Exit(1)
	}
}

// buildRegistry wires one collector per configured server onto a fresh
// registry. Split out of main so the whole config → registry → /metrics path
// is reachable from a test.
func buildRegistry(cfg *config.Config) (*prometheus.Registry, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	for _, sc := range cfg.Servers {
		client, err := adguard.New(sc)
		if err != nil {
			return nil, fmt.Errorf("adguard client %s: %w", sc.Name, err)
		}
		// Two mechanisms label this collector's output with `server`, and
		// BOTH are required — neither is a future "simplification" of the
		// other:
		//
		//  1. NewWithLabels, NEVER New. Every server shares the namespace
		//     "adguard", so their _scrape_collector_success/_duration_seconds
		//     gauges would otherwise be label-identical. collector.Collector
		//     is unchecked, so the registry does not reject that at Register
		//     time — it fails the whole Gather, taking every metric on the
		//     endpoint down with it. But NewWithLabels only reaches those two
		//     self-monitoring gauges; it has no way to touch the DEVICE
		//     samples Source.Collect returns.
		//  2. adguard.NewSource's name argument (below) stamps `server` onto every device
		//     sample itself, inside Samples. Without it, two instances'
		//     otherwise-identical scalar and topN series (e.g.
		//     adguard_dns_queries_24h) collide at Gather the same way the
		//     self-monitoring gauges would — silently dropping one server's
		//     entire payload, scrape after scrape.
		reg.MustRegister(collector.NewWithLabels(
			adguard.NewSource(client, sc.Name),
			cfg.Timeout,
			prometheus.Labels{"server": sc.Name},
		))
		slog.Info("adguard collector enabled", "server", sc.Name, "url", sc.URL)
	}

	return reg, nil
}

// metricsHandler serves reg at the telemetry path.
//
// ErrorHandling is ContinueOnError, and that is load-bearing. The zero value
// is HTTPErrorOnError, under which ANY error from a single Gather — a
// duplicate series from one collector, or a panic inside one — is answered
// with HTTP 500 and an empty body, throwing away every metric on the
// endpoint: the healthy server's whole namespace, the Go and process series,
// and both _scrape_collector_success gauges, so the very alerts meant to
// catch a broken collector go absent exactly when they are needed.
//
// ContinueOnError serves everything that gathered cleanly with HTTP 200 and
// records the failure on
// promhttp_metric_handler_errors_total{cause="gathering"} — partial data plus
// a counter saying so. Registry is set so that counter exists on this
// endpoint at all.
func metricsHandler(reg *prometheus.Registry) http.Handler {
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:      reg,
		ErrorHandling: promhttp.ContinueOnError,
	})
	return promhttp.InstrumentMetricHandler(reg, h)
}
