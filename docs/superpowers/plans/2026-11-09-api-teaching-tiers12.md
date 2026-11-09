# API-тренажёр для студентов (ярусы 1–2) — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Превратить сервер в тренажёр для обучения работе с HTTP API: эмуляция 500/задержек/битого JSON, X-Request-Id, машиночитаемая спецификация API и готовые примеры клиентов.

**Architecture:** Тот же сервер `server/main.go` (stdlib): эмуляции — три независимых счётчика на базе существующего `limiter`; X-Request-Id — в middleware `logRequests`; openapi.json — go:embed; examples/ — клиенты только на стандартных библиотеках (нулевой порог входа: смысл — HTTP-семантика, не SDK).

**Tech Stack:** Go 1.26 stdlib; Python 3 (urllib), Node (fetch), Go — для examples; POSIX sh.

**Spec:** плана-спецификации нет — объём согласован в диалоге (ярусы 1–2), задачи самодостаточны.

## Global Constraints

- Комментарии, README, сообщения об ошибках — на русском (`.bat` — ASCII).
- Только stdlib; `CGO_ENABLED=0`.
- Обратная совместимость флагов: `-emulate-429` продолжает работать как раньше.
- Эмуляции (`429`, `500`, broken-json, `-slow`) не действуют на `GET /health` и `GET /v1/models`.
- Grammar спецификаций эмуляции едина: `every:N`, `percent:P`, `first:N`, `always`.
- Examples — только стандартные библиотеки языков (без pip/npm-зависимостей), комментарии на русском.

## Review Focus

1. **Эмуляции не должны рвать health-чеки**: `/health` и `/v1/models` отвечают 200 без заголовков эмуляции при `-emulate-500 always`. Тест в Task 2.
2. **429 и 500 считаются независимыми счётчиками** — `-emulate-429 every:2 -emulate-500 every:2` даёт чередование 429/500/успех, а не вечные ошибки. Тест в Task 2.
3. **`-slow` не должен блокировать отклонённые (429/500) и не должен висеть при отмене клиента** — sleep с `r.Context()`. Тест в Task 3.
4. **broken-json — это 200 с невалидным телом**: клиент должен уметь это обнаружить. Тест в Task 4.
5. **Входящий X-Request-Id от клиента пробрасывается в ответ** (корреляция клиент↔лог), сгенерированный — новый hex. Тесты в Task 1.

---

### Task 1: X-Request-Id

**Files:**
- Modify: `server/main.go` (`logRequests`)
- Test: `server/main_test.go`

**Interfaces:**
- Produces: middleware `logRequests`: в каждый ответ (включая ошибки) ставится `X-Request-Id`; если запрос пришёл со своим `X-Request-Id` — он пробрасывается, иначе генерируется 8-hex из crypto/rand; id попадает в строку лога.

- [ ] **Step 1: Тесты**: (а) ответ на любой запрос содержит непустой `X-Request-Id`; (б) входящий id возвращается без изменений; (в) id попадает в лог — проверяем через лог-выход? Проще: два разных запроса без входящего id получают разные значения.

- [ ] **Step 2:** FAIL; реализация в `logRequests`.

- [ ] **Step 3:** PASS; коммит `server: X-Request-Id (эхо входящего, генерация, в лог)`.

### Task 2: -emulate-500

**Files:**
- Modify: `server/main.go`
- Test: `server/main_test.go`

**Interfaces:**
- Consumes: `parseEmulate429`/`limiter` (переименовать в `parseEmulateSpec` — тот же синтаксис и поведение; `parseEmulate429` остаётся тонкой обёрткой или удаляется вместе с обновлением всех вызовов).
- Produces: флаг `-emulate-500` (та же grammar); ответ 500 в формате OpenAI: `"type":"server_error"`, `"code":"server_error"`, сообщение `Internal server error (emulated)`; заголовок `Retry-After` не ставится.
- Порядок проверок в обработчике: 500-лимитер проверяется после 429-лимитера, до остального.

- [ ] **Step 1: Тесты**: (а) `-emulate-500 always` → 500 с `"type":"server_error"`, без `Retry-After`; (б) чередование: два лимитера `every:2` на первых шести запросах дают паттерн `[429,500,ok,429,500,ok]`-подобное чередование — проверяем, что счётчики независимы (не «вечно ошибки»); (в) `/health` при `always` → 200.

- [ ] **Step 2:** FAIL; реализация (переименование + новый лимитер + ветка в `transcribeTask`).

- [ ] **Step 3:** PASS; коммит `server: -emulate-500 — тренажёр неотличения 429 от 500`.

### Task 3: -slow N

**Files:**
- Modify: `server/main.go`
- Test: `server/integration_test.go`

**Interfaces:**
- Produces: флаг `-slow` (int, секунды, по умолчанию 0); поле `slow time.Duration` в `server`; sleep выполняется в `transcribeTask` после лимитеров, до парсинга формы; через `r.Context()` — отмена клиента прерывает; 429/500 не задерживаются.

- [ ] **Step 1: Тесты** (на заглушках, slow=1): (а) ответ на транскрипцию занимает ≥1 c; (б) запрос, отклонённый 429-лимитером при том же `-slow`, отвечает быстро (<500 мс).

- [ ] **Step 2:** FAIL; реализация.

- [ ] **Step 3:** PASS; коммит `server: -slow — тренажёр таймаутов клиента`.

### Task 4: -emulate-broken-json

**Files:**
- Modify: `server/main.go`
- Test: `server/main_test.go`

**Interfaces:**
- Produces: флаг `-emulate-broken-json` (та же grammar); в `transcribeTask` для `response_format=json`, когда лимитер сработал: 200, Content-Type json, тело — обрезанный JSON (`{"text": "<начало текста"` без закрывающей скобки/кавычки). Остальные response_format не трогаются.

- [ ] **Step 1: Тесты**: (а) при `always` тело не парсится как JSON и статус 200; (б) при пустом флаге тело валидно; (в) health не задет.

- [ ] **Step 2:** FAIL; реализация.

- [ ] **Step 3:** PASS; коммит `server: -emulate-broken-json — тренажёр валидации ответа`.

### Task 5: GET /v1/openapi.json

**Files:**
- Create: `server/openapi.json` (OpenAPI 3.0,Paths: /health, /v1/models, /v1/audio/transcriptions, /v1/audio/translations; multipart `file` + `language`/`response_format`/`prompt`; форматы ошибок OpenAI)
- Modify: `server/main.go` (маршрут + go:embed)
- Test: `server/main_test.go`

**Interfaces:**
- Produces: `//go:embed openapi.json`; `mux.HandleFunc("GET /v1/openapi.json", ...)` отдаёт сырой файл с Content-Type `application/json`.

- [ ] **Step 1: Тесты**: (а) GET → 200, Content-Type json; (б) тело парсится как JSON, есть `openapi` и все четыре пути в `paths`.

- [ ] **Step 2:** FAIL; реализация (spec писать по факту поведения сервера, не по мечте: `-emulate-*` флаги в description эндпоинтов).

- [ ] **Step 3:** PASS (сам файл дополнительно валидируется `ruby -ryaml`/json.tool); коммит `server: OpenAPI-спецификация на /v1/openapi.json`.

### Task 6: examples/ — клиенты на stdlib

**Files:**
- Create: `examples/transcribe.py`, `examples/transcribe.js`, `examples/transcribe.go`, `examples/README.md`

**Interfaces:**
- Produces: каждый пример — один файл: транскрипция файла с URL/таймаутом из аргументов, ретраи на 429 (чтение Retry-After + экспоненциальный backoff, максимум 5), отказ ретраить 500, обнаружение broken-json (json parse), печать текста. Комментарии на русском объясняют каждый шаг.
- `examples/README.md`: как запускать каждый, как поднять сервер-тренажёр (`-emulate-429 every:2 -emulate-500 first:3 -slow 5`), какие навыки отрабатывает.

- [ ] **Step 1:** Написать три клиента + README (каждый клиент самодостаточен, аргументы: URL сервера, путь к файлу, [язык]).

- [ ] **Step 2: Проверка**: поднять сервер на заглушках (как в смоук-тесте) с `-emulate-429 every:2 -emulate-500 always`; запустить `transcribe.py` и `transcribe.go` против него: py/go должны (а) отработать 429-ретрай, (б) ответить понятным сообщением на 500 и не зациклиться. Node: `node --check transcribe.js` если node установлен, иначе верификация чтением (ruling).

- [ ] **Step 3:** Коммит `examples: клиенты на stdlib с ретраями и обработкой ошибок`.

### Task 7: build.sh — examples в пакеты + README

**Files:**
- Modify: `build.sh` (`stage()`: `cp -r "$here/examples" "$d/examples"`)
- Modify: `README.md`, `README.dist.md`

- [ ] **Step 1:** build.sh: примеры кладутся в пакет; README.md: таблица флагов (+`-emulate-500`, `-slow`, `-emulate-broken-json`), упоминание `X-Request-Id`, `/v1/openapi.json`, раздел «Примеры клиентов» → examples/; README.dist.md: то же кратко (в пакете examples лежат рядом).

- [ ] **Step 2:** Коммит `build+README: примеры в пакеты, новые флаги и openapi в доках`.

### Task 8: Финальная проверка

- [ ] **Step 1:** `cd server && go vet ./... && go test ./...` — зелёные.
- [ ] **Step 2:** `sh launcher/test-transcribe.sh` — зелёные.
- [ ] **Step 3:** Смоук реального бинарника: сервер с заглушками + `-emulate-429 every:2 -emulate-500 first:1 -slow 1 -emulate-broken-json percent:0`; curl: /health с X-Request-Id (эхо), чередование 429/500, /v1/openapi.json валиден.
- [ ] **Step 4:** Коммит (если остались правки) — обычно не нужен.
