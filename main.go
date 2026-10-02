// Command sitewatch probes websites and exposes the results to Prometheus.
//
//	sitewatch serve -targets sites.txt          run the exporter
//	sitewatch check example.com tunis.tn        probe once and print a table
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(os.Args[2:]))
	case "check":
		os.Exit(check(os.Args[2:]))
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n  sitewatch serve [-listen :9115] [-interval 30s] [-timeout 10s] -targets FILE [URL...]\n  sitewatch check [-timeout 10s] URL...\n  sitewatch version")
}

func loadTargets(file string, extra []string) ([]string, error) {
	var targets []string
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if targets, err = ParseTargets(f); err != nil {
			return nil, err
		}
	}
	more, err := ParseTargets(strings.NewReader(strings.Join(extra, "\n")))
	if err != nil {
		return nil, err
	}
	targets = append(targets, more...)
	if len(targets) == 0 {
		return nil, errors.New("no targets: pass -targets FILE or URLs")
	}
	return targets, nil
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", ":9115", "address to listen on")
	interval := fs.Duration("interval", 30*time.Second, "time between probe rounds")
	timeout := fs.Duration("timeout", 10*time.Second, "per-request timeout")
	file := fs.String("targets", "", "file with one URL per line")
	_ = fs.Parse(args)

	targets, err := loadTargets(*file, fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitewatch:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wt := NewWatcher(NewChecker(*timeout), targets, *interval)
	go wt.Run(ctx)

	srv := &http.Server{Addr: *listen, Handler: Handler(wt), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	fmt.Printf("sitewatch %s watching %d targets every %s on %s\n", version, len(targets), *interval, *listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "sitewatch:", err)
		return 1
	}
	return 0
}

// Handler serves /metrics, /targets (JSON) and /healthz.
func Handler(wt *Watcher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		WriteMetrics(w, wt)
	})
	mux.HandleFunc("/targets", func(w http.ResponseWriter, _ *http.Request) {
		type row struct {
			Target    string   `json:"target"`
			Up        bool     `json:"up"`
			Status    int      `json:"status"`
			Seconds   float64  `json:"seconds"`
			CertDays  *float64 `json:"cert_days_left,omitempty"`
			Error     string   `json:"error,omitempty"`
			CheckedAt string   `json:"checked_at"`
		}
		var rows []row
		for _, r := range wt.Snapshot() {
			x := row{r.Target, r.Up, r.Status, r.Total.Seconds(), nil, r.Err, r.CheckedAt.UTC().Format(time.RFC3339)}
			if d, ok := r.CertDaysLeft(); ok {
				x.CertDays = &d
			}
			rows = append(rows, x)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	return mux
}

func check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	timeout := fs.Duration("timeout", 10*time.Second, "per-request timeout")
	_ = fs.Parse(args)
	targets, err := loadTargets("", fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitewatch:", err)
		return 2
	}
	c := NewChecker(*timeout)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tSTATUS\tTOTAL\tTTFB\tCERT DAYS\tNOTE")
	down := 0
	for _, t := range targets {
		r := c.Check(context.Background(), t)
		cert := "-"
		if d, ok := r.CertDaysLeft(); ok {
			cert = fmt.Sprintf("%.0f", d)
		}
		note := r.Err
		if !r.Up {
			down++
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", t, r.Status, r.Total.Round(time.Millisecond), r.TTFB.Round(time.Millisecond), cert, note)
	}
	tw.Flush()
	if down > 0 {
		return 1
	}
	return 0
}
