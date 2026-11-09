// Интеграционные тесты: полный путь запроса через заглушки ffmpeg и
// whisper-cli (sh-скрипты), без реальных бинарников и модели.
package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	// и пишет json-результат рядом с -of <base>.
	stubWhisper = `#!/bin/sh
{ for a in "$@"; do echo "$a"; done; } >> "$ARGS_FILE"
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
