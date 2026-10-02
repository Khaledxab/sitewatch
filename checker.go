package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"time"
)

// Result is the outcome of one probe of one target.
type Result struct {
	Target     string
	Up         bool
	Status     int
	Err        string
	DNS        time.Duration
	Connect    time.Duration
	TLS        time.Duration
	TTFB       time.Duration
	Total      time.Duration
	CertExpiry time.Time
	CheckedAt  time.Time
}

// CertDaysLeft returns the days until the leaf certificate expires, and false
// when the target was not served over TLS.
func (r Result) CertDaysLeft() (float64, bool) {
	if r.CertExpiry.IsZero() {
		return 0, false
	}
	return time.Until(r.CertExpiry).Hours() / 24, true
}

// Checker probes URLs. The zero value is not usable: use NewChecker.
type Checker struct {
	Client *http.Client
}

// NewChecker builds a Checker with a per-request timeout and a redirect limit.
func NewChecker(timeout time.Duration) *Checker {
	return &Checker{Client: &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

// Check performs one GET and records the timing of each phase. A target is up
// when the request completes and the final status is below 400.
func (c *Checker) Check(ctx context.Context, target string) Result {
	res := Result{Target: target, CheckedAt: time.Now()}
	var dnsStart, connStart, tlsStart time.Time
	start := time.Now()

	trace := &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { res.DNS += time.Since(dnsStart) },
		ConnectStart:         func(_, _ string) { connStart = time.Now() },
		ConnectDone:          func(_, _ string, _ error) { res.Connect += time.Since(connStart) },
		TLSHandshakeStart:    func() { tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { res.TLS += time.Since(tlsStart) },
		GotFirstResponseByte: func() { res.TTFB = time.Since(start) },
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, target, nil)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	req.Header.Set("User-Agent", "sitewatch/1.0 (+https://github.com/Khaledxab/sitewatch)")

	resp, err := c.Client.Do(req)
	res.Total = time.Since(start)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.Status = resp.StatusCode
	res.Up = resp.StatusCode < 400
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		res.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter
	}
	return res
}
