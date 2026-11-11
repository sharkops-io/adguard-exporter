# adguard-exporter

A Prometheus exporter for [AdGuard Home](https://github.com/AdguardTeam/AdGuardHome).
It polls AdGuard's own control API — the same API the AdGuard Home web UI
uses — and republishes it as Prometheus exposition. It supports any number of
AdGuard Home instances from a single process, each distinguished by a
`server` label.

## Why this exists: AdGuard Home has no `/metrics`

AdGuard Home does not expose Prometheus metrics itself. Hitting `/metrics`
against a real instance returns:

- **404** if you're authenticated — the path simply doesn't exist.
- **401** if you're not — AdGuard's auth middleware answers before routing
  even reaches the point of returning a 404, so an unauthenticated probe
  looks like the endpoint might exist behind a login. It doesn't.

This exporter authenticates with HTTP basic auth against AdGuard's real
control endpoints (`/control/status`, `/control/stats`,
`/control/stats/config`) and turns the JSON they return into Prometheus
series.

## Gauges, not counters — read this before you write a `rate()`

**Every metric this exporter publishes is a gauge.** AdGuard's `Num*`
statistics (`num_dns_queries`, `num_blocked_filtering`, and so on) are
**totals over a rolling statistics window** — 24 hours by default, not
lifetime counters. As older buckets age out of that window, these numbers
**go down**. There is no monotonic counter anywhere in AdGuard's stats API
for this exporter to expose.

`rate()` and `increase()` on any `adguard_*_24h` or `adguard_top_*` series
will produce numbers that look plausible and mean nothing — a resolver that
handled a perfectly steady stream of queries can show a negative "rate" the
moment the window rolls. Graph these as gauges (plain values, `avg_over_time`,
etc.), not as counters.

## Configuration

Multiple servers are configured as **comma-separated positional lists** —
URLs, names, usernames and passwords line up by index. The names, usernames
and passwords lists must each be the same length as the URLs list (names may
instead be omitted entirely, in which case each server's URL is used as its
name). **Get the order wrong and each server's metrics are silently
attributed to a different server's name** — there is no cross-check possible
against the position alone, only against the count.

Flags take precedence over the matching environment variable, which exists
so a Helm values file or a `docker-compose.yml` can stay fully declarative.

| Flag | Environment | Default | Description |
|---|---|---|---|
| `--web.listen-address` | — | `:9618` | Address to listen on. |
| `--web.telemetry-path` | — | `/metrics` | Path under which to expose metrics. |
| `--collector.timeout` | — | `25s` | Per-collector timeout for a whole scrape. Keep below Prometheus' `scrape_timeout`. |
| `--http.timeout` | — | `10s` | Per-HTTP-request timeout. A scrape of one server makes **three** sequential requests (status, stats, stats config), so 3×`--http.timeout` must fit inside `--collector.timeout`, which must itself fit inside Prometheus' `scrape_timeout`. With the shipped defaults (10s / 25s), a server taking more than ~8s per request can exhaust the collector timeout — if you're scraping a slow instance, lower `--http.timeout` (e.g. to `8s`) rather than raising `--collector.timeout` above `scrape_timeout`. |
| `--log.level` | — | `info` | `debug`, `info`, `warn` or `error`. |
| `--log.format` | — | `logfmt` | `logfmt` or `json`. |
| `--adguard.servers` | `ADGUARD_SERVERS` | — | Comma-separated AdGuard Home base URLs. Required. |
| `--adguard.names` | `ADGUARD_NAMES` | URL | Comma-separated server labels, positionally matching `--adguard.servers`. |
| `--adguard.usernames` | `ADGUARD_USERNAMES` | — | Comma-separated usernames, positionally matching `--adguard.servers`. Required. |
| `--adguard.passwords` | `ADGUARD_PASSWORDS` | — | Comma-separated passwords, positionally matching `--adguard.servers`. Required. |
| `--adguard.insecure-skip-verify` | — | `false` | Disable TLS verification for all servers. |

Server names must be **unique**. Two AdGuard instances sharing a name would
otherwise collide into one label-identical series — the exporter checks this
at startup and refuses to start on a duplicate, rather than serving a
registry that silently drops one instance's entire payload at scrape time.

Run `adguard-exporter --help` for the same list generated straight from the
binary.

### Example

```bash
docker run --rm -p 9618:9618 \
  -e ADGUARD_SERVERS="https://adguard-1.example.internal,https://adguard-2.example.internal" \
  -e ADGUARD_NAMES="site-a,site-b" \
  -e ADGUARD_USERNAMES="exporter,exporter" \
  -e ADGUARD_PASSWORDS="secret-a,secret-b" \
  adguard-exporter:local
```

```yaml
# docker-compose.yml
services:
  adguard-exporter:
    image: adguard-exporter:local
    build: .
    ports:
      - "9618:9618"
    environment:
      ADGUARD_SERVERS: "https://adguard-1.example.internal,https://adguard-2.example.internal"
      ADGUARD_NAMES: "site-a,site-b"
      ADGUARD_USERNAMES: "exporter,exporter"
      ADGUARD_PASSWORDS: "secret-a,secret-b"
    restart: unless-stopped
```

Then scrape `http://localhost:9618/metrics`.

There is deliberately no `--adguard.insecure-skip-verify` per-server toggle:
it applies to every configured server at once. If one instance needs it and
another doesn't, run two exporter instances.

## Metric reference

Generated by hand from `internal/adguard/samples.go` and
`internal/adguard/types.go` — the source of truth for what this exporter
actually emits.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `adguard_running` | gauge | `server` | Whether AdGuard Home reports itself as running (1) or not (0). |
| `adguard_protection_enabled` | gauge | `server` | Whether DNS filtering is enabled (1) or disabled (0). |
| `adguard_build_info` | gauge | `server`, `version` | AdGuard Home build information; always 1, read the `version` label. |
| `adguard_stats_window_seconds` | gauge | `server` | Width of the AdGuard statistics window, in seconds. The `_24h` metrics are only correctly named while this is `86400`. |
| `adguard_dns_queries_24h` | gauge | `server` | DNS queries handled during the statistics window. A rolling window, not a counter. |
| `adguard_blocked_filtered_24h` | gauge | `server` | Queries blocked by filter lists during the statistics window. |
| `adguard_replaced_safebrowsing_24h` | gauge | `server` | Queries replaced by safe browsing during the statistics window. |
| `adguard_replaced_safesearch_24h` | gauge | `server` | Queries replaced by safe search during the statistics window. |
| `adguard_replaced_parental_24h` | gauge | `server` | Queries replaced by parental filtering during the statistics window. |
| `adguard_avg_processing_time_seconds` | gauge | `server` | Mean query processing time over the statistics window, **in seconds**. |
| `adguard_dns_queries_last_hour` | gauge | `server` | DNS queries during the last complete hour. Omitted when the statistics window isn't hourly buckets (see below). |
| `adguard_blocked_filtered_last_hour` | gauge | `server` | Queries blocked by filter lists during the last complete hour. Omitted under the same condition. |
| `adguard_top_clients` | gauge | `server`, `client` | Queries per client during the statistics window. |
| `adguard_top_upstreams_responses` | gauge | `server`, `upstream` | Responses per upstream resolver during the statistics window. |
| `adguard_top_upstreams_avg_time_seconds` | gauge | `server`, `upstream` | Mean response time per upstream resolver, **in seconds**. |
| `adguard_scrape_collector_success` | gauge | `server` | Whether the last collection for this server succeeded (1) or failed (0). |
| `adguard_scrape_collector_duration_seconds` | gauge | `server` | Duration of the last collection for this server, in seconds. |

The endpoint also serves `go_*` and `process_*` (the standard Go runtime and
process collectors) and `promhttp_*` (the handler's own instrumentation,
including `promhttp_metric_handler_errors_total{cause="gathering"}` — the
signal a partial `/metrics` response leaves behind; see below).

### `avg_processing_time` is in seconds, not milliseconds

`adguard_avg_processing_time_seconds` and `adguard_top_upstreams_avg_time_seconds`
are both **seconds**. AdGuard's control API returns them that way, but at
least one other AdGuard exporter names its equivalent metric with a
`_milliseconds` suffix and is wrong by a factor of 1000. A "processing time"
panel copied verbatim from that project onto this exporter's data will be off
by three orders of magnitude. Check the unit suffix, not just the metric
name, when porting a dashboard.

### `_last_hour` metrics read the last *complete* hour, and can disappear

AdGuard returns its hourly buckets 24-wide, oldest first. Index 23 is the
current, still-filling hour — reading it would show traffic apparently
collapsing every time the clock crosses an hour boundary — so
`adguard_dns_queries_last_hour` and `adguard_blocked_filtered_last_hour` read
index 22, the last bucket that's actually finished.

These two series are **omitted entirely** when AdGuard's statistics interval
is configured above 24 hours: at that point AdGuard switches to returning
daily buckets instead of hourly ones, and a metric still named `_last_hour`
would be describing something it no longer is. `adguard_stats_window_seconds`
is exported specifically so this condition is detectable from Prometheus
rather than only from AdGuard's own settings UI — `examples/rules.yaml`'s
`AdGuardStatsWindowUnexpected` alert fires when it isn't `86400`.

## No query-log and no per-domain metrics — deliberately

AdGuard's stats API also returns `top_queried_domains` and
`top_blocked_domains`: up to 100 domain names per instance, whose label
values churn continuously as new domains are queried and old ones fall out
of the window. This exporter does not publish them, and does not expose
anything derived from the query log.

That's a cardinality decision, not an oversight. A `domain` label with an
effectively unbounded, constantly-rotating set of values grows a time series
database without limit — every new domain ever queried becomes a brand-new
series, forever. This is not a hypothetical: it is the documented root cause
of [henrywhitaker3/adguard-exporter#70](https://github.com/henrywhitaker3/adguard-exporter/issues/70),
where that project's `/metrics` endpoint deadlocks under exactly this kind of
growth. This exporter publishes `adguard_top_clients` (bounded by AdGuard's
own top-N list, keyed by client rather than by domain) and nothing
domain-shaped, and has no plan to add it.

## Three things that are not obvious

**One bad collection does not blank the endpoint.** `/metrics` is served
with `promhttp.ContinueOnError`: if one server's collector fails, the
response is still HTTP 200 carrying everything that gathered cleanly — every
other configured server's whole namespace, the runtime series, and every
`adguard_scrape_collector_success` gauge (including the failing server's own,
now reading `0`), so the alerts in `examples/rules.yaml` can still fire. The
failure additionally increments
`promhttp_metric_handler_errors_total{cause="gathering"}`.

**TLS verification is on by default.** `--adguard.insecure-skip-verify`
defaults to `false` and applies to every configured server. If an AdGuard
Home instance serves a self-signed certificate, collection for it will fail
outright until either the certificate is trusted or this flag is set.

**There is deliberately no `/healthz` endpoint.** A liveness/readiness probe
that actually collected from AdGuard would couple this pod's restarts to
AdGuard being reachable at that instant. A broken exporter should surface as
a `DOWN` Prometheus scrape target (the `up` metric, plus
`adguard_scrape_collector_success` per server) rather than as a probe
result. If you need a Kubernetes probe, point it at TCP-connect on the listen
port instead.

## Dashboards and alerts

`examples/rules.yaml` has six alerting rules: exporter reachability
(`AdGuardExporterDown`), per-server collection health
(`AdGuardCollectorFailing`), AdGuard reporting itself stopped
(`AdGuardDown`), filtering left disabled (`AdGuardProtectionDisabled`), slow
resolution (`AdGuardSlowProcessing`), and the statistics-window sanity check
described above (`AdGuardStatsWindowUnexpected`). CI runs
`promtool check rules` and `promtool test rules` against it on every change.

## Building

```bash
docker build -t adguard-exporter:local .
```

CI builds the image with Docker Buildx through the shared
[`image.yml`](https://github.com/sharkops-io/.github/blob/main/.github/workflows/image.yml)
workflow and pushes it to `ghcr.io/sharkops-io/adguard-exporter`: `:main` and
`:sha-*` from `main`, `:vX.Y.Z` and `:latest` from a release tag. It builds
`linux/amd64` only, which matches the target cluster; an arm64 host needs a
local `docker build`.

## License


Apache License 2.0. See [LICENSE](LICENSE).
