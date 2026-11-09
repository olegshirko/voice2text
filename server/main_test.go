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

func TestHealthVersion(t *testing.T) {
	s := &server{t: &transcriber{model: "m.bin"}}
	rec := httptest.NewRecorder()
	s.health(rec, httptest.NewRequest("GET", "/health", nil))
	if !contains(rec.Body.String(), `"version":"dev"`) {
		t.Errorf("health must report version, got %s", rec.Body.String())
	}
}

func TestTranslationsRequiresFile(t *testing.T) {
	s := &server{t: &transcriber{model: "m.bin"}}
	var buf strings.Builder
	buf.WriteString("--X\r\nContent-Disposition: form-data; name=\"language\"\r\n\r\nen\r\n--X--\r\n")
	req := httptest.NewRequest("POST", "/v1/audio/translations", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=X")
	rec := httptest.NewRecorder()
	s.translate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if !contains(rec.Body.String(), `"code":"missing_file"`) {
		t.Errorf("body %s lacks missing_file", rec.Body.String())
	}
}

func TestExtractDetectedLang(t *testing.T) {
	// Точный формат строки из whisper.cpp v1.9.4 (src/whisper.cpp:6956).
	out := "whisper_full_with_state: auto-detected language: ru (p = 0.999)"
	if got := extractDetectedLang(out); got != "ru" {
		t.Errorf("extractDetectedLang = %q, want ru", got)
	}
	if got := extractDetectedLang("some other output\nwithout the line"); got != "" {
		t.Errorf("extractDetectedLang = %q, want empty", got)
	}
}

func TestVerboseUsesDetectedLang(t *testing.T) {
	r := &result{segments: []segment{{0, 1500, "x"}}, detectedLang: "de"}
	v := r.verbose("auto")
	if v["language"] != "de" {
		t.Errorf("verbose(auto).language = %v, want de", v["language"])
	}
	if v := (&result{segments: []segment{{0, 1500, "x"}}}).verbose("auto"); v["language"] != "auto" {
		t.Errorf("verbose(auto) without detection: language = %v, want auto", v["language"])
	}
	if v := r.verbose("ru"); v["language"] != "ru" {
		t.Errorf("verbose(ru).language = %v, want ru", v["language"])
	}
}

func TestRequestIDGenerated(t *testing.T) {
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest("GET", "/health", nil))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/health", nil))
	id1, id2 := rec1.Header().Get("X-Request-Id"), rec2.Header().Get("X-Request-Id")
	if id1 == "" || id2 == "" {
		t.Fatalf("X-Request-Id must be set, got %q and %q", id1, id2)
	}
	if id1 == id2 {
		t.Errorf("two requests got the same id %q", id1)
	}
}

func TestRequestIDEcho(t *testing.T) {
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-Id", "my-id-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "my-id-123" {
		t.Errorf("X-Request-Id = %q, want echoed my-id-123", got)
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
