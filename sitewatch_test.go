package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckUpAndDown(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("hi")) }))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer bad.Close()

	c := NewChecker(2 * time.Second)
	if r := c.Check(context.Background(), ok.URL); !r.Up || r.Status != 200 {
		t.Fatalf("expected up with 200, got %+v", r)
	}
	if r := c.Check(context.Background(), bad.URL); r.Up || r.Status != 500 {
		t.Fatalf("expected down with 500, got %+v", r)
	}
}

func TestCheckUnreachable(t *testing.T) {
	c := NewChecker(500 * time.Millisecond)
	r := c.Check(context.Background(), "http://127.0.0.1:1")
	if r.Up || r.Err == "" {
		t.Fatalf("expected a failed probe with an error, got %+v", r)
	}
}

func TestCheckRedirectsAreFollowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	redir := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusMovedPermanently))
	defer redir.Close()
	if r := NewChecker(2*time.Second).Check(context.Background(), redir.URL); !r.Up || r.Status != 200 {
		t.Fatalf("expected the redirect to be followed, got %+v", r)
	}
}

func TestCheckReadsCertificateExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := &Checker{Client: srv.Client()}
	r := c.Check(context.Background(), srv.URL)
	if !r.Up {
		t.Fatalf("expected up, got %+v", r)
	}
	days, ok := r.CertDaysLeft()
	if !ok || days <= 0 {
		t.Fatalf("expected a future expiry, got %v %v", days, ok)
	}
	if r.TLS <= 0 {
		t.Fatalf("expected a TLS handshake time, got %v", r.TLS)
	}
}

func TestParseTargets(t *testing.T) {
	in := "# comment\n\nexample.com\nhttps://example.com\nhttp://plain.test/path\n  spaced.tn  \n"
	got, err := ParseTargets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com", "http://plain.test/path", "https://spaced.tn"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMetricsOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	wt := NewWatcher(NewChecker(2*time.Second), []string{srv.URL}, time.Minute)
	wt.round(context.Background())
	wt.round(context.Background())

	rec := httptest.NewRecorder()
	Handler(wt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`sitewatch_up{target="` + srv.URL + `"} 1`,
		`sitewatch_http_status_code{target="` + srv.URL + `"} 200`,
		`sitewatch_checks_total{target="` + srv.URL + `",outcome="success"} 2`,
		`sitewatch_checks_total{target="` + srv.URL + `",outcome="failure"} 0`,
		"# TYPE sitewatch_response_seconds gauge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "sitewatch_tls_cert_expiry_days{") {
		t.Error("plain HTTP target must not report a certificate expiry")
	}
}

func TestLabelEscaping(t *testing.T) {
	if got := labelEscaper.Replace("a\"b\\c\nd"); got != `a\"b\\c\nd` {
		t.Fatalf("bad escaping: %q", got)
	}
}
