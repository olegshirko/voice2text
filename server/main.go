// voice2text-server — HTTP-сервер распознавания речи, совместимый с
// OpenAI Audio API (POST /v1/audio/transcriptions). Под капотом ffmpeg и
// whisper-cli из соседнего каталога bin/.
//
// Флаг -emulate-429 включает имитацию ответа 429 Too Many Requests, чтобы
// проверять, как клиенты переживают rate limit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	root := defaultRoot()
	var (
		addr        = flag.String("addr", "127.0.0.1:8080", "адрес и порт, например 0.0.0.0:8080")
		model       = flag.String("model", "", "путь к ggml-модели (по умолчанию первая ggml-*.bin в models/)")
		binDir      = flag.String("bin", filepath.Join(root, "bin"), "каталог с whisper-cli и ffmpeg")
		lang        = flag.String("lang", "ru", "язык по умолчанию, если клиент не передал language")
		threads     = flag.Int("threads", runtime.NumCPU(), "потоков для whisper-cli")
		concurrency = flag.Int("concurrency", 1, "сколько запросов распознаются одновременно, остальные ждут")
		emulate429  = flag.String("emulate-429", "", "имитировать 429: every:N (каждый N-й запрос), percent:P (P% запросов), first:N (первые N), always")
		retryAfter  = flag.Int("retry-after", 20, "заголовок Retry-After в секундах для имитируемых 429")
	)
	flag.Parse()

	limiter, err := parseEmulate429(*emulate429)
	if err != nil {
		log.Fatalf("-emulate-429: %v", err)
	}
	if *model == "" {
		*model, err = findModel(filepath.Join(root, "models"))
		if err != nil {
			log.Fatal(err)
		}
	}

	t := &transcriber{
		whisper: filepath.Join(*binDir, exe("whisper-cli")),
		ffmpeg:  filepath.Join(*binDir, ffmpegName()),
		model:   *model,
		lang:    *lang,
		threads: *threads,
		slots:   make(chan struct{}, *concurrency),
	}
	for _, p := range []string{t.whisper, t.ffmpeg, t.model} {
		if _, err := os.Stat(p); err != nil {
			log.Fatal(err)
		}
	}

	s := &server{t: t, limiter: limiter, retryAfter: *retryAfter}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("POST /v1/audio/transcriptions", s.transcribe)
	mux.HandleFunc("POST /inference", s.transcribe) // как у whisper-server

	log.Printf("voice2text-server: model=%s lang=%s threads=%d concurrency=%d", filepath.Base(t.model), t.lang, t.threads, *concurrency)
	if limiter != nil {
		log.Printf("emulating 429: %s, Retry-After: %d", *emulate429, *retryAfter)
	}
	log.Printf("listening on http://%s  (POST /v1/audio/transcriptions)", *addr)
	log.Fatal(http.ListenAndServe(*addr, logRequests(mux)))
}

// --- имитация 429 -----------------------------------------------------------

// limiter решает, отдать ли на очередной запрос 429.
type limiter struct {
	mode    string // every | percent | first | always
	n       int
	counter atomic.Int64
}

func parseEmulate429(spec string) (*limiter, error) {
	if spec == "" {
		return nil, nil
	}
	if spec == "always" {
		return &limiter{mode: "always"}, nil
	}
	mode, arg, ok := strings.Cut(spec, ":")
	if !ok {
		return nil, fmt.Errorf("ожидается every:N, percent:P, first:N или always, получено %q", spec)
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%s: число должно быть неотрицательным, получено %q", mode, arg)
	}
	switch mode {
	case "every":
		if n == 0 {
			return nil, errors.New("every:0 не имеет смысла")
		}
	case "percent":
		if n > 100 {
			return nil, errors.New("percent: не больше 100")
		}
	case "first":
	default:
		return nil, fmt.Errorf("неизвестный режим %q", mode)
	}
	return &limiter{mode: mode, n: n}, nil
}

// reject возвращает true, если этот запрос надо отклонить с 429.
func (l *limiter) reject() bool {
	if l == nil {
		return false
	}
	i := l.counter.Add(1) // номер запроса, с единицы
	switch l.mode {
	case "always":
		return true
	case "every":
		return i%int64(l.n) == 0
	case "first":
		return i <= int64(l.n)
	case "percent":
		// Детерминированно: из каждых 100 запросов первые P получают 429.
		return (i-1)%100 < int64(l.n)
	}
	return false
}

// --- HTTP -------------------------------------------------------------------

type server struct {
	t          *transcriber
	limiter    *limiter
	retryAfter int
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "model": filepath.Base(s.t.model)})
}

func (s *server) models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"id": "whisper-1", "object": "model", "owned_by": "voice2text",
			"file": filepath.Base(s.t.model),
		}},
	})
}

func (s *server) transcribe(w http.ResponseWriter, r *http.Request) {
	if s.limiter.reject() {
		w.Header().Set("Retry-After", strconv.Itoa(s.retryAfter))
		w.Header().Set("x-ratelimit-limit-requests", "60")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.Header().Set("x-ratelimit-reset-requests", strconv.Itoa(s.retryAfter)+"s")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", "rate_limit_exceeded",
			fmt.Sprintf("Rate limit reached for requests. Please try again in %ds.", s.retryAfter))
		return
	}

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "", "expected multipart/form-data: "+err.Error())
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "missing_file", "field 'file' is required")
		return
	}
	defer file.Close()

	format := r.FormValue("response_format")
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "text", "srt", "vtt", "verbose_json":
	default:
		writeError(w, http.StatusBadRequest, "invalid_request_error", "", "unsupported response_format "+format)
		return
	}
	lang := r.FormValue("language")
	if lang == "" {
		lang = s.t.lang
	}

	res, err := s.t.run(r.Context(), file, filepath.Ext(hdr.Filename), lang, r.FormValue("prompt"))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("transcribe %s: %v", hdr.Filename, err)
		writeError(w, http.StatusInternalServerError, "server_error", "", err.Error())
		return
	}

	switch format {
	case "json":
		writeJSON(w, http.StatusOK, map[string]any{"text": res.text()})
	case "verbose_json":
		writeJSON(w, http.StatusOK, res.verbose(lang))
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, res.text()+"\n")
	case "srt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, res.srt())
	case "vtt":
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		io.WriteString(w, res.vtt())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError отвечает в формате ошибок OpenAI.
func writeError(w http.ResponseWriter, status int, typ, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"message": msg, "type": typ, "code": code, "param": nil,
	}})
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

// --- распознавание ----------------------------------------------------------

type transcriber struct {
	whisper, ffmpeg, model, lang string
	threads                      int
	slots                        chan struct{}
}

type segment struct {
	From, To int64 // миллисекунды
	Text     string
}

type result struct {
	segments []segment
}

func (t *transcriber) run(ctx context.Context, src io.Reader, ext, lang, prompt string) (*result, error) {
	dir, err := os.MkdirTemp("", "voice2text-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "input"+ext)
	f, err := os.Create(in)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

	// Очередь: whisper-cli в несколько экземпляров только мешают друг другу.
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	wav := filepath.Join(dir, "audio.wav")
	cmd := exec.CommandContext(ctx, t.ffmpeg, "-v", "error", "-y", "-i", in, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(out)))
	}

	outBase := filepath.Join(dir, "out")
	args := []string{"-m", t.model, "-l", lang, "-t", strconv.Itoa(t.threads), "-f", wav, "-oj", "-of", outBase, "-np"}
	if prompt != "" {
		args = append(args, "--prompt", prompt)
	}
	cmd = exec.CommandContext(ctx, t.whisper, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("whisper-cli: %s", lastLines(string(out), 5))
	}

	raw, err := os.ReadFile(outBase + ".json")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Transcription []struct {
			Offsets struct{ From, To int64 } `json:"offsets"`
			Text    string                   `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("whisper json: %w", err)
	}
	res := &result{}
	for _, s := range parsed.Transcription {
		res.segments = append(res.segments, segment{s.Offsets.From, s.Offsets.To, strings.TrimSpace(s.Text)})
	}
	return res, nil
}

func (r *result) text() string {
	parts := make([]string, 0, len(r.segments))
	for _, s := range r.segments {
		if s.Text != "" {
			parts = append(parts, s.Text)
		}
	}
	return strings.Join(parts, " ")
}

func (r *result) verbose(lang string) map[string]any {
	segs := make([]map[string]any, 0, len(r.segments))
	var dur float64
	for i, s := range r.segments {
		segs = append(segs, map[string]any{
			"id": i, "start": ms(s.From), "end": ms(s.To), "text": s.Text,
		})
		dur = ms(s.To)
	}
	return map[string]any{
		"task": "transcribe", "language": lang, "duration": dur,
		"text": r.text(), "segments": segs,
	}
}

func (r *result) srt() string {
	var b strings.Builder
	for i, s := range r.segments {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, stamp(s.From, ","), stamp(s.To, ","), s.Text)
	}
	return b.String()
}

func (r *result) vtt() string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, s := range r.segments {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", stamp(s.From, "."), stamp(s.To, "."), s.Text)
	}
	return b.String()
}

func ms(v int64) float64 { return float64(v) / 1000 }

func stamp(v int64, sep string) string {
	h, v := v/3600000, v%3600000
	m, v := v/60000, v%60000
	s, msec := v/1000, v%1000
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", h, m, s, sep, msec)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// --- расположение файлов ----------------------------------------------------

// defaultRoot — каталог пакета: там, где лежит сам бинарник.
func defaultRoot() string {
	if p, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Dir(p)
	}
	return "."
}

func findModel(dir string) (string, error) {
	m, _ := filepath.Glob(filepath.Join(dir, "ggml-*.bin"))
	if len(m) == 0 {
		return "", fmt.Errorf("no ggml-*.bin model in %s (use -model)", dir)
	}
	return m[0], nil
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func ffmpegName() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "ffmpeg-arm64"
		}
		return "ffmpeg-x64"
	default:
		return exe("ffmpeg")
	}
}
