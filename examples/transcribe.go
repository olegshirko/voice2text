// Клиент voice2text на net/http: без зависимостей, только стандартная
// библиотека. Показывает полный набор навыков работы с HTTP API:
//
//	go run transcribe.go http://127.0.0.1:8080 lecture.mp4 [ru]
//
// - загрузка файла через mime/multipart
// - ретраи на 429 (Retry-After + экспоненциальный backoff, максимум 5 попыток)
// - отказ ретраить 500
// - таймаут через http.Client{Timeout} и контекст
// - валидация тела ответа: 200 не гарантирует корректный JSON
//   (сервер имитирует: ./serve.sh -emulate-broken-json always)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const maxAttempts = 5

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ошибка: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func main() {
	if len(os.Args) < 3 {
		fail("usage: %s <server-url> <file> [lang]", os.Args[0])
	}
	base, path := os.Args[1], os.Args[2]
	lang := "ru"
	if len(os.Args) > 3 {
		lang = os.Args[3]
	}

	// http.Client с таймаутом на весь запрос: без него зависший сервер
	// подвесит клиента. Строка лога сервера несёт тот же X-Request-Id.
	client := &http.Client{Timeout: 5 * time.Minute}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, contentType, err := multipartBody(path, lang)
		if err != nil {
			fail("%v", err)
		}
		req, err := http.NewRequest(http.MethodPost, base+"/v1/audio/transcriptions", body)
		if err != nil {
			fail("%v", err)
		}
		req.Header.Set("Content-Type", contentType)
		reqID := fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
		req.Header.Set("X-Request-Id", reqID) // сервер вернёт его в ответе

		resp, err := client.Do(req)
		if err != nil {
			fail("сервер недоступен: %v", err)
		}

		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			fmt.Fprintf(os.Stderr, "X-Request-Id: %s\n", resp.Header.Get("X-Request-Id"))
			// Валидация тела: 200 не гарантирует корректный JSON.
			var out struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				fail("сервер вернул 200 с некорректным JSON — проверьте -emulate-broken-json")
			}
			fmt.Println(out.Text)
			return
		case http.StatusTooManyRequests:
			// Rate limit: Retry-After от сервера + экспоненциальный запас.
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			wait += 1 << attempt
			fmt.Fprintf(os.Stderr, "429, попытка %d/%d, ждём %dс...\n", attempt, maxAttempts, wait)
			time.Sleep(time.Duration(wait) * time.Second)
		default:
			// 500 и прочие: повтор обычно бессмыслен, ломаемся сразу.
			var out struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
			if json.Unmarshal(raw, &out) == nil && out.Error.Message != "" {
				msg = out.Error.Message
			}
			fail("%s (%d)", msg, resp.StatusCode)
		}
	}
	fail("исчерпаны %d попыток", maxAttempts)
}

// multipartBody собирает multipart/form-data с полями file и language.
func multipartBody(path, lang string) (*bytes.Reader, string, error) {
	audio, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("не могу прочитать %s: %w", path, err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	fw.Write(audio)
	w.WriteField("language", lang)
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return bytes.NewReader(buf.Bytes()), w.FormDataContentType(), nil
}
