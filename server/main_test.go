package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEmulate429(t *testing.T) {
	for _, bad := range []string{"every", "every:0", "every:x", "percent:101", "sometimes:3", "first:-1"} {
		if _, err := parseEmulate429(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	if l, err := parseEmulate429(""); err != nil || l != nil {
		t.Errorf("empty spec must disable emulation, got %v %v", l, err)
	}
}

func TestLimiterPattern(t *testing.T) {
	cases := []struct {
		spec string
		want []bool // для первых 6 запросов
	}{
		{"always", []bool{true, true, true, true, true, true}},
		{"every:3", []bool{false, false, true, false, false, true}},
		{"first:2", []bool{true, true, false, false, false, false}},
		{"percent:0", []bool{false, false, false, false, false, false}},
		{"percent:100", []bool{true, true, true, true, true, true}},
		{"percent:50", []bool{true, true, true, true, true, true}}, // первые 50 из каждой сотни
	}
	for _, c := range cases {
		l, err := parseEmulate429(c.spec)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range c.want {
			if got := l.reject(); got != want {
				t.Errorf("%s: request %d: got %v want %v", c.spec, i+1, got, want)
			}
		}
	}
}

func TestPercentOverHundred(t *testing.T) {
	l, _ := parseEmulate429("percent:30")
	rejected := 0
	for i := 0; i < 1000; i++ {
		if l.reject() {
			rejected++
		}
	}
	if rejected != 300 {
		t.Errorf("percent:30 over 1000 requests: rejected %d, want 300", rejected)
	}
}

func TestTranscribe429Response(t *testing.T) {
	l, _ := parseEmulate429("always")
	s := &server{limiter: l, retryAfter: 7}
	rec := httptest.NewRecorder()
	s.transcribe(rec, httptest.NewRequest("POST", "/v1/audio/transcriptions", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "7" {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"rate_limit_error"`, `"code":"rate_limit_exceeded"`, "try again in 7s"} {
		if !contains(body, want) {
			t.Errorf("body %s lacks %s", body, want)
		}
	}
}

func TestHealthNotRateLimited(t *testing.T) {
	l, _ := parseEmulate429("always")
	s := &server{limiter: l, t: &transcriber{model: "m.bin"}}
	rec := httptest.NewRecorder()
	s.health(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Errorf("health must not be rate limited, got %d", rec.Code)
	}
}

func TestFormats(t *testing.T) {
	r := &result{segments: []segment{{0, 1500, "Привет"}, {1500, 3725, "мир"}}}
	if got := r.text(); got != "Привет мир" {
		t.Errorf("text = %q", got)
	}
	if got := r.srt(); got != "1\n00:00:00,000 --> 00:00:01,500\nПривет\n\n2\n00:00:01,500 --> 00:00:03,725\nмир\n\n" {
		t.Errorf("srt = %q", got)
	}
	if got := r.vtt(); got != "WEBVTT\n\n00:00:00.000 --> 00:00:01.500\nПривет\n\n00:00:01.500 --> 00:00:03.725\nмир\n\n" {
		t.Errorf("vtt = %q", got)
	}
	v := r.verbose("ru")
	if v["duration"] != 3.725 || len(v["segments"].([]map[string]any)) != 2 {
		t.Errorf("verbose = %v", v)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
