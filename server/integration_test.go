// Интеграционные тесты: полный путь запроса через заглушки ffmpeg и
// whisper-cli (sh-скрипты), без реальных бинарников и модели.
package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// wavHeader — минимальный валидный RIFF/WAVE-заголовок 16-битного моно PCM.
const wavHeader = "RIFF\x24\x08\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x00\x7d\x00\x00\x00\x7d\x00\x00\x01\x00\x08\x00data"

// stubScripts возвращает содержимое заглушек; ARGS_FILE задают через env.
const (
	stubFFmpeg = `#!/bin/sh
for out; do :; done
printf '%s' '` + wavHeader + `' > "$out"
printf '%.0sX' $(seq 1 2048) >> "$out"
`

	// whisper-cli дописывает каждый аргумент отдельной строкой в $ARGS_FILE
	// и пишет json-результат рядом с -of <base>. Без -np реальный whisper-cli
	// печатает в stderr строку auto-detected language — заглушка повторяет это.
	stubWhisper = `#!/bin/sh
{ for a in "$@"; do echo "$a"; done; } >> "$ARGS_FILE"
case " $* " in
  *" -np "*) ;;
  *) echo "whisper_full_with_state: auto-detected language: ru (p = 0.999)" >&2 ;;
esac
prev=
for a in "$@"; do
  [ "$prev" = "-of" ] && out="$a"
  prev="$a"
done
cat > "$out.json" <<'JSON'
{"transcription":[{"offsets":{"from":0,"to":1500},"text":" Привет"},{"offsets":{"from":1500,"to":3725},"text":" мир"}]}
JSON
`
)

func newStubServer(t *testing.T) *server {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("заглушки — sh-скрипты")
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{exe("whisper-cli"): stubWhisper, ffmpegName(): stubFFmpeg} {
		p := filepath.Join(bin, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	model := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(model, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return &server{t: &transcriber{
		whisper: filepath.Join(bin, exe("whisper-cli")),
		ffmpeg:  filepath.Join(bin, ffmpegName()),
		model:   model,
		lang:    "ru",
		threads: 2,
		slots:   make(chan struct{}, 1),
	}}
}

// multipartFile собирает multipart/form-data с полями file и extra (значение или "").
func multipartFile(t *testing.T, filename string, extra map[string]string) (*bytes.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", filename)
	fw.Write([]byte("fake audio bytes"))
	for k, v := range extra {
		w.WriteField(k, v)
	}
	w.Close()
	return bytes.NewReader(buf.Bytes()), w.FormDataContentType()
}

func stubArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("args file: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func assertArgs(t *testing.T, args []string, want [][2]string, notWantFlags ...string) {
	t.Helper()
	for _, pair := range want {
		if !hasPair(args, pair[0], pair[1]) {
			t.Errorf("args %v: want %s %s", args, pair[0], pair[1])
		}
	}
	for _, flag := range notWantFlags {
		if hasFlag(args, flag) {
			t.Errorf("args %v: unexpected %s", args, flag)
		}
	}
}

func TestTranscribeEndToEnd(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)

	body, ct := multipartFile(t, "a.mp3", map[string]string{})
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.transcribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{\"text\":\"Привет мир\"}\n" {
		t.Errorf("body = %q", got)
	}
	args := stubArgs(t, argsFile)
	assertArgs(t, args, [][2]string{{"-l", "ru"}}, "-tr", "--prompt")
	wavInput := false
	for i, a := range args {
		if a == "-f" && i+1 < len(args) && strings.HasSuffix(args[i+1], ".wav") {
			wavInput = true
		}
	}
	if !wavInput {
		t.Errorf("args %v: -f must point to a .wav file", args)
	}
	if hasFlag(args, "-tr") {
		t.Errorf("transcriptions must not pass -tr: %v", args)
	}
	if hasFlag(args, "--prompt") {
		t.Errorf("no prompt given, got %v", args)
	}
}

func TestTranslateEndToEnd(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)

	body, ct := multipartFile(t, "a.mp3", map[string]string{"language": "en"})
	req := httptest.NewRequest("POST", "/v1/audio/translations", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.translate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	args := stubArgs(t, argsFile)
	if !hasFlag(args, "-tr") {
		t.Errorf("translations must pass -tr: %v", args)
	}
	if !hasPair(args, "-l", "en") {
		t.Errorf("args %v: want -l en", args)
	}
}

func TestPromptPassthrough(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)

	body, ct := multipartFile(t, "a.mp3", map[string]string{"prompt": "X"})
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.transcribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	args := stubArgs(t, argsFile)
	if !hasPair(args, "--prompt", "X") {
		t.Errorf("args %v: want --prompt X", args)
	}
}

func TestAutoDetectLang(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)

	body, ct := multipartFile(t, "a.mp3", map[string]string{"language": "auto", "response_format": "verbose_json"})
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.transcribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), `"language":"ru"`) {
		t.Errorf("verbose_json(auto) must report detected language, got %s", rec.Body.String())
	}
	args := stubArgs(t, argsFile)
	if hasFlag(args, "-np") {
		t.Errorf("with language=auto, -np must be omitted so whisper-cli reports the detected language: %v", args)
	}
}

func TestEmulateBrokenJSON(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)
	broken, _ := parseEmulateSpec("always")
	s.brokenJSON = broken

	body, ct := multipartFile(t, "a.mp3", nil)
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.transcribe(rec, req)

	// 200 с невалидным телом — клиент обязан это обнаружить сам.
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err == nil {
		t.Errorf("body must be broken json, parsed fine: %s", rec.Body.String())
	}
}

func TestSlowDelayAndFast429(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	s := newStubServer(t)
	s.slow = time.Second
	l429, _ := parseEmulateSpec("first:1")
	s.limiter = l429

	// Первый запрос отклоняется 429 — быстро, задержка не применяется.
	start := time.Now()
	body, ct := multipartFile(t, "a.mp3", nil)
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.transcribe(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("first request: status %d, want 429", rec.Code)
	}
	if d := time.Since(start); d >= 500*time.Millisecond {
		t.Errorf("429 must not be delayed by -slow, took %v", d)
	}

	// Второй проходит — с задержкой не меньше -slow.
	start = time.Now()
	body, ct = multipartFile(t, "a.mp3", nil)
	req = httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec = httptest.NewRecorder()
	s.transcribe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second request: status %d: %s", rec.Code, rec.Body.String())
	}
	if d := time.Since(start); d < time.Second {
		t.Errorf("-slow must delay the response, took only %v", d)
	}
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func hasPair(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}
