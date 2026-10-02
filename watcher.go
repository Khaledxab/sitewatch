package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Watcher probes a fixed set of targets on an interval and keeps the latest
// result plus running success and failure counts for each.
type Watcher struct {
	checker  *Checker
	targets  []string
	interval time.Duration

	mu      sync.RWMutex
	results map[string]Result
	counts  map[string]map[string]uint64
}

func NewWatcher(c *Checker, targets []string, interval time.Duration) *Watcher {
	return &Watcher{
		checker:  c,
		targets:  targets,
		interval: interval,
		results:  make(map[string]Result),
		counts:   make(map[string]map[string]uint64),
	}
}

// Run probes every target immediately, then once per interval, until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.round(ctx)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.round(ctx)
		}
	}
}

func (w *Watcher) round(ctx context.Context) {
	var wg sync.WaitGroup
	for _, target := range w.targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			w.record(w.checker.Check(ctx, target))
		}(target)
	}
	wg.Wait()
}

func (w *Watcher) record(r Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.results[r.Target] = r
	if w.counts[r.Target] == nil {
		w.counts[r.Target] = map[string]uint64{}
	}
	outcome := "success"
	if !r.Up {
		outcome = "failure"
	}
	w.counts[r.Target][outcome]++
}

// Snapshot returns the latest results sorted by target.
func (w *Watcher) Snapshot() []Result {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Result, 0, len(w.results))
	for _, r := range w.results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// Counts returns a copy of the success and failure counters for a target.
func (w *Watcher) Counts(target string) (success, failure uint64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.counts[target]["success"], w.counts[target]["failure"]
}

// ParseTargets reads one URL per line. Blank lines and lines starting with #
// are ignored. A bare host such as example.com becomes https://example.com.
func ParseTargets(r io.Reader) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "https://" + line
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading targets: %w", err)
	}
	return out, nil
}
