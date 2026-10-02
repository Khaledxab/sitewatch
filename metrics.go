package main

import (
	"fmt"
	"io"
	"strings"
)

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

type family struct {
	name, help, typ string
}

// WriteMetrics renders the watcher state in the Prometheus text exposition
// format. It has no dependencies: the format is simple and stable.
func WriteMetrics(w io.Writer, wt *Watcher) {
	results := wt.Snapshot()

	gauge := func(f family, value func(Result) (float64, bool)) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.typ)
		for _, r := range results {
			if v, ok := value(r); ok {
				fmt.Fprintf(w, "%s{target=\"%s\"} %g\n", f.name, labelEscaper.Replace(r.Target), v)
			}
		}
	}
	secs := func(pick func(Result) float64) func(Result) (float64, bool) {
		return func(r Result) (float64, bool) { return pick(r), true }
	}

	gauge(family{"sitewatch_up", "1 if the last probe succeeded (status below 400), else 0.", "gauge"},
		func(r Result) (float64, bool) {
			if r.Up {
				return 1, true
			}
			return 0, true
		})
	gauge(family{"sitewatch_http_status_code", "HTTP status of the last probe, 0 when no response.", "gauge"},
		secs(func(r Result) float64 { return float64(r.Status) }))
	gauge(family{"sitewatch_response_seconds", "Total time of the last probe.", "gauge"},
		secs(func(r Result) float64 { return r.Total.Seconds() }))
	gauge(family{"sitewatch_dns_seconds", "DNS lookup time of the last probe.", "gauge"},
		secs(func(r Result) float64 { return r.DNS.Seconds() }))
	gauge(family{"sitewatch_connect_seconds", "TCP connect time of the last probe.", "gauge"},
		secs(func(r Result) float64 { return r.Connect.Seconds() }))
	gauge(family{"sitewatch_tls_handshake_seconds", "TLS handshake time of the last probe.", "gauge"},
		secs(func(r Result) float64 { return r.TLS.Seconds() }))
	gauge(family{"sitewatch_time_to_first_byte_seconds", "Time to first response byte of the last probe.", "gauge"},
		secs(func(r Result) float64 { return r.TTFB.Seconds() }))
	gauge(family{"sitewatch_tls_cert_expiry_days", "Days until the served TLS certificate expires. Absent for plain HTTP.", "gauge"},
		func(r Result) (float64, bool) { return r.CertDaysLeft() })
	gauge(family{"sitewatch_last_check_timestamp_seconds", "Unix time of the last probe.", "gauge"},
		secs(func(r Result) float64 { return float64(r.CheckedAt.Unix()) }))

	fmt.Fprint(w, "# HELP sitewatch_checks_total Probes run, by outcome.\n# TYPE sitewatch_checks_total counter\n")
	for _, r := range results {
		ok, bad := wt.Counts(r.Target)
		t := labelEscaper.Replace(r.Target)
		fmt.Fprintf(w, "sitewatch_checks_total{target=\"%s\",outcome=\"success\"} %d\n", t, ok)
		fmt.Fprintf(w, "sitewatch_checks_total{target=\"%s\",outcome=\"failure\"} %d\n", t, bad)
	}
}
