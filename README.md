# sitewatch

A small website monitor written in Go. It probes a list of URLs and exposes what it finds to Prometheus: is the site up, how long each phase of the request took, and how many days are left on its TLS certificate.

I wrote it because I run production sites for clients and wanted the three things that actually wake me up (down, slow, expiring certificate) in one tiny binary, without running a full monitoring product for them.

- One static binary, standard library only, no dependencies.
- 15 MB Docker image on a distroless base, runs as non-root.
- Helm chart for Kubernetes (k3s, EKS), with an optional ServiceMonitor.
- A Grafana dashboard and example alert rules included.

## Quick start

```sh
go build -o sitewatch .
./sitewatch check khaledxab.com example.com
```

```
TARGET                  STATUS  TOTAL  TTFB   CERT DAYS  NOTE
https://khaledxab.com   200     234ms  234ms  75
https://example.com     200     118ms  118ms  41
```

`check` exits with code 1 when any target is down, so it also works in a cron job or a CI step.

Run it as an exporter:

```sh
./sitewatch serve -targets targets.example.txt -interval 30s
curl localhost:9115/metrics
curl localhost:9115/targets     # the same data as JSON
```

## Metrics

| Metric | Meaning |
|---|---|
| `sitewatch_up` | 1 when the last probe finished with a status below 400 |
| `sitewatch_http_status_code` | status of the last probe, 0 when there was no response |
| `sitewatch_response_seconds` | total time of the last probe |
| `sitewatch_dns_seconds`, `_connect_seconds`, `_tls_handshake_seconds` | time spent in each phase |
| `sitewatch_time_to_first_byte_seconds` | time until the server started answering |
| `sitewatch_tls_cert_expiry_days` | days until the certificate expires (absent for plain HTTP) |
| `sitewatch_checks_total{outcome}` | probes run, split into success and failure |

## Kubernetes

```sh
helm install sitewatch deploy/helm/sitewatch \
  --set 'targets={khaledxab.com,hesabi.tn}' \
  --set serviceMonitor.enabled=true
```

Import `dashboards/sitewatch.json` into Grafana and load `alerts.example.yml` into Prometheus for the three alerts I care about: site down for 2 minutes, certificate under 14 days, first byte slower than 2 seconds.

## Design notes

- Probes run concurrently each round, so one slow site never delays the others.
- A target is "up" when the final status after up to 5 redirects is below 400. Keep-alive is off on purpose, so every probe measures a fresh DNS lookup, connection and handshake, which is what a new visitor pays.
- The metrics text is written by hand instead of pulling in a client library. The format is small and stable, and it keeps the binary dependency free.
- Results live in memory. Prometheus is the history, so there is no database to run.

## Development

```sh
make test     # go vet and go test -race
make docker
```

MIT licensed.
