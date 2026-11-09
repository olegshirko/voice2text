# Улучшения voice2text — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть главные пробелы проекта: второй эндпоинт OpenAI Audio API (переводы), выбор модели в transcribe, таймауты сервера, CI с релизами и sha256, версия в /health, интеграционный тест, тестовый сэмпл записи.

**Architecture:** Весь Go-код — в `server/main.go` (один файл, stdlib). Эндпоинт переводов — тот же обработчик с параметром task, флаг `-tr` в whisper-cli. Интеграционный тест подменяет `bin/` заглушками-скриптами. Лаунчеры — POSIX sh и cmd.exe, выбор модели через env `VOICE2TEXT_MODEL`.

**Tech Stack:** Go 1.26 (stdlib), POSIX sh, cmd.exe batch, GitHub Actions (ubuntu для тестов, macos-latest для сборки релизов).

**Spec:** плана-спецификации нет — объём согласован в диалоге (ядро + мелочи + сэмпл), задачи ниже самодостаточны.

## Global Constraints

- Комментарии, README и сообщения об ошибках — на русском (файлы `.bat` — только ASCII).
- Сервер: только stdlib, `CGO_ENABLED=0`.
- Лаунчеры: `transcribe.sh`/`serve.sh` — POSIX sh; `transcribe.bat`/`serve.bat` — cmd.exe, CRLF добавляется на этапе сборки (`build.sh`), в репозитории LF.
- `build.sh` не меняет поведение, кроме добавления версии в ldflags (Task 1).
- Формат версии: `YYYYMMDD` (уже так в build.sh), в бинарнике по умолчанию `dev`.
- Тесты сервера не должны требовать реальных ffmpeg/whisper-cli/модели.

## Review Focus

1. **`/v1/audio/translations` без `file`** → 400 в формате ошибок OpenAI, как у transcriptions. Тест в Task 2.
2. **`-tr` не должен попадать в обычный `/v1/audio/transcriptions`** — интеграционный тест сверяет аргументы заглушки whisper-cli точно (Task 4).
3. **`VOICE2TEXT_MODEL` с несуществующим именем** → понятная ошибка со списком каталога `models/`, а не тихий fallback (Task 6).
4. **Таймауты не должны рвать долгие загрузки** — `ReadTimeout` остаётся 0, ставится только `ReadHeaderTimeout` и `IdleTimeout` (Task 3, проверка кодом, не тестом).
5. **`extractDetectedLang` на выводе без строки auto-detect** → пустая строка, verbose_json отдаёт запрошенный язык как раньше (Task 5).

---

### Task 1: Версия сервера в /health

**Files:**
- Modify: `server/main.go`
- Modify: `build.sh` (функция `build_server`)
- Test: `server/main_test.go`

**Interfaces:**
- Produces: `var version = "dev"` (переопределяется ldflags `-X main.version=...`); `GET /health` возвращает поле `"version"`.

- [ ] **Step 1: Тест — дополнить TestHealthNotRateLimited или новый TestHealthVersion**: ответ `/health` содержит `"version"` (значение `dev` в тестах).

- [ ] **Step 2:** `go test ./...` в `server/` — ожидаем FAIL.

- [ ] **Step 3:** В `main.go`: `var version = "dev"`; в `health` добавить `"version": version` в ответ.

- [ ] **Step 4:** В `build.sh` в `build_server()` добавить `-X main.version=$VERSION` в `-ldflags`.

- [ ] **Step 5:** Тесты PASS; коммит `server: версия сборки в GET /health`.

### Task 2: Эндпоинт /v1/audio/translations

**Files:**
- Modify: `server/main.go`
- Test: `server/main_test.go`

**Interfaces:**
- Produces: `func (t *transcriber) run(ctx context.Context, src io.Reader, ext, task, lang, prompt string) (*result, error)` — новый параметр `task` (`"transcribe" | "translate"`); при `translate` в args whisper-cli добавляется `-tr`.
- Маршрут: `mux.HandleFunc("POST /v1/audio/translations", ...)` — тот же обработчик с `task=translate` (OpenAI всегда переводит на английский).

- [ ] **Step 1: Тест TestTranslationsRequiresFile**: POST `/v1/audio/translations` без multipart → 400, тело содержит `"missing_file"`. (Код лимитера перед этим не мешает: limiter == nil.)

- [ ] **Step 2:** тест FAIL (маршрута нет).

- [ ] **Step 3:** Реализация: обработчик — замыкание `transcribeHandler(task string)`; в `run` добавить `task`, при `"translate"` — `-tr` в args. README не трогаем (Task 9).

- [ ] **Step 4:** тесты PASS; коммит `server: POST /v1/audio/translations (OpenAI-совместимый перевод)`.

### Task 3: Таймауты http.Server

**Files:**
- Modify: `server/main.go` (конец main)

- [ ] **Step 1:** Заменить `http.ListenAndServe(*addr, logRequests(mux))` на `http.Server{Addr: *addr, Handler: logRequests(mux), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}.ListenAndServe()`. `ReadTimeout`/`WriteTimeout` — 0 (загрузки и распознавание долгие).

- [ ] **Step 2:** `go vet ./... && go test ./...` PASS; коммит `server: таймауты http.Server (ReadHeaderTimeout, IdleTimeout)`.

### Task 4: Интеграционный тест с заглушками ffmpeg/whisper-cli

**Files:**
- Create: `server/integration_test.go`

**Interfaces:**
- Consumes: маршруты из Task 2, `transcriber` c полями `whisper, ffmpeg, model, lang, threads, slots`, `ffmpegName()`.
- Produces: `newStubServer(t, whisperArgsFile string) *server` — тестовая фабрика (каталог с заглушками, model-файл-пустышка, лимитер nil).

- [ ] **Step 1:** Скиппить на Windows (`runtime.GOOS == "windows"` — заглушки это sh-скрипты).

- [ ] **Step 2:** Заглушки:
  - `ffmpeg` (имя через `ffmpegName()` на darwin, иначе `ffmpeg`): берёт последний аргумент как путь вывода, создаёт там валидный wav (44-байтный RIFF-заголовок + N байт данных, константы прямо в тесте).
  - `whisper-cli`: парсит `-of <base>`, пишет `<base>.json` с двумя сегментами («Привет», «мир»), все свои аргументы (по одному на строку) дописывает в файл из env `ARGS_FILE`.
- [ ] **Step 3: Тест TestTranscribeEndToEnd**: POST `/v1/audio/transcriptions` с multipart (поле `file`, имя `a.mp3`) → 200, `{"text": "Привет мир"}`; аргументы заглушки: `language=ru`, нет `-tr`, `--prompt` отсутствует.
- [ ] **Step 4: Тест TestTranslateEndToEnd**: POST `/v1/audio/translations` с `language=en` → 200; в аргументах есть `-tr` и `-l en`.
- [ ] **Step 5: Тест TestPromptPassthrough**: с полем `prompt=X` → в аргументах `--prompt X`.
- [ ] **Step 6:** тесты PASS; коммит `server: интеграционный тест с заглушками ffmpeg/whisper-cli`.

### Task 5: auto-detected language в verbose_json

**Files:**
- Modify: `server/main.go`
- Test: `server/main_test.go`

**Interfaces:**
- Produces: `func extractDetectedLang(output string) string` — regexp `auto-detected language (\w+)`, иначе `""`. Поле `result.detectedLang`; `verbose()` использует его, когда клиент просил `auto`.

- [ ] **Step 1: Тесты extractDetectedLang**: строка `whisper_full_with_state: auto-detected language = ru (p = 0.999)` → `"ru"`; вывод без такой строки → `""`.
- [ ] **Step 2:** FAIL; реализация; в `run()` заполнять `res.detectedLang` из CombinedOutput whisper-cli; в `verbose(lang)`: если `lang == "auto" && r.detectedLang != ""` — отдавать его в поле `language`.
- [ ] **Step 3:** PASS; коммит `server: определённый язык в verbose_json при language=auto`.

### Task 6: Выбор модели в transcribe — VOICE2TEXT_MODEL

**Files:**
- Modify: `launcher/transcribe.sh`, `launcher/transcribe.bat`
- Test: `launcher/test-transcribe.sh` (новый, sh, запускается на macOS/Linux)

**Interfaces:**
- Produces: правило резолва модели (одно и то же в .sh и .bat): если `VOICE2TEXT_MODEL` задан — при наличии `/` это путь к файлу, иначе шаблон `models/ggml-$VOICE2TEXT_MODEL*.bin`; если не задан или не найден — первая `ggml-*.bin`. Несуществующее имя → ошибка с перечислением `models/`.

- [ ] **Step 1: test-transcribe.sh**: собирает в mktemp-каталоге скелет пакета: `bin/ffmpeg` (заглушка — создаёт wav, как в Task 4), `bin/whisper-cli` (заглушка — пишет свои аргументы в stdout), `models/ggml-small-test.bin` и `models/ggml-large-test.bin` (пустые). Запускает `transcribe.sh` из каталога репозитория с `VOICE2TEXT_MODEL` и проверяет, какой `-m ...` получил заглушка. Кейсы: не задан → large; `small` → small; `small-test` → small; `../x/nonexist` → ненулевой код и сообщение про models/.
- [ ] **Step 2:** прогнать `sh launcher/test-transcribe.sh` — реализовать резолв в `transcribe.sh` до PASS.
- [ ] **Step 3:** то же правило в `transcribe.bat` (проверка только кодом: `%VOICE2TEXT_MODEL%`, синтаксис `for %%f in ("%HERE%models\ggml-%VOICE2TEXT_MODEL%*.bin")`, без `/` — путь передаётся как есть; ASCII).
- [ ] **Step 4:** коммит `transcribe: выбор модели через VOICE2TEXT_MODEL`.

### Task 7: Сэмпл записи для проверки

**Files:**
- Create: `sample/sample-ru.m4a` (коммитится)
- Modify: `README.md`, `README.dist.md` (краткий блок «Проверка», полный текст в Task 9)

- [ ] **Step 1:** Сгенерировать: `say -v <русский голос> -o /tmp/sample-ru.aiff "Привет! Это тестовая запись для проверки офлайн распознавания речи."` (голос выбрать через `say -v '?' | grep ru`), затем `afconvert -f m4af -d aac /tmp/sample-ru.aiff sample/sample-ru.m4a`. Размер — десятки КБ.
- [ ] **Step 2:** Проверить реально: whisper-cli из `.cache/whisper.cpp/build/bin/whisper-cli` + модель из `.cache/models/` на сэмпле — в тексте есть «тестовая запись».
- [ ] **Step 3:** Коммит `sample: тестовая запись для быстрой проверки`.

### Task 8: serve.bat — собственная подпрограмма vcredist

**Files:**
- Modify: `launcher/serve.bat`

- [ ] **Step 1:** Перенести `:vcredist` из transcribe.bat в serve.bat (та же проверка `vcruntime140_1.dll`/`msvcp140.dll`, curl с aka.ms, ветка ARM64 по `%PROCESSOR_ARCHITECTURE%`), убрать вызов `call "%~dp0transcribe.bat" --vcredist-only`. Флаг `--vcredist-only` в transcribe.bat оставить.
- [ ] **Step 2:** Коммит `serve.bat: установка VC++ runtime без захода в transcribe.bat`.

### Task 9: CI — GitHub Actions

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1:** Схема:
  - `test` (ubuntu-latest, на push в main и PR): `actions/setup-go` (go-version-file: server/go.mod), `cd server && go vet ./... && go test ./...`, `shellcheck launcher/*.sh launcher/test-transcribe.sh`.
  - `release` (macos-latest, только на тег `v*`, `needs: test`, `permissions: contents: write`): checkout, setup-go, `./build.sh`, `cd dist && shasum -a 256 *.zip > SHA256SUMS.txt`, `gh release create "$GITHUB_REF_NAME" dist/*.zip dist/SHA256SUMS.txt` с `GH_TOKEN: ${{ github.token }}`.
- [ ] **Step 2:** Валидация: `go vet`/тесты локально уже зелёные; yaml валиден (проверка `ruby -e` или `python3 -c "import yaml..."` — если PyYAML нет, просто внимательная вычитка).
- [ ] **Step 3:** Коммит `ci: тесты на каждый push, сборка и релиз по тегу с sha256`.

### Task 10: README

**Files:**
- Modify: `README.md`, `README.dist.md`

- [ ] **Step 1:** README.md: раздел про `/v1/audio/translations` (curl-пример с `-F task`... нет — отдельный путь), `VOICE2TEXT_MODEL` в таблице/тексте использования, `version` в описании `/health`, блок «Проверка» с сэмплом (`./transcribe.sh sample/sample-ru.m4a`), упоминание CI-релизов в «Сборке».
- [ ] **Step 2:** README.dist.md: то же, кратко, без сборочных частей.
- [ ] **Step 3:** Коммит `README: translations, VOICE2TEXT_MODEL, сэмпл, версия в /health`.

### Task 11: Финальная проверка

- [ ] **Step 1:** `cd server && go vet ./... && go test ./...` — все тесты зелёные.
- [ ] **Step 2:** `sh launcher/test-transcribe.sh` — PASS.
- [ ] **Step 3:** `./build.sh macos` — сервер собирается с версией, архив обновляется в dist/ (опционально, если пользователь захочет пересобрать пакет).
