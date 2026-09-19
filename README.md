# voice2text — автономные сборки speech-to-text

Готовые архивы для macOS, Linux и Windows лежат в
[Releases](https://github.com/olegshirko/voice2text/releases): распаковать
и запустить, ничего ставить не нужно. Инструкция для пользователя внутри
каждого архива и в [README.dist.md](README.dist.md).

Сборочный скрипт, который делает из whisper.cpp, модели Whisper и
статического ffmpeg пять самодостаточных архивов:

| Архив | Что внутри |
|---|---|
| `voice2text-macos-*.zip` | универсальный whisper-cli (arm64 + x86_64, Metal), собран здесь из исходников |
| `voice2text-linux-x64-*.zip`, `-linux-arm64-*.zip` | бинарники из релиза whisper.cpp (ubuntu-22.04) |
| `voice2text-windows-x64-*.zip`, `-windows-arm64-*.zip` | бинарники из релиза whisper.cpp (MSVC) |

Каждый архив содержит `bin/` (whisper-cli, ffmpeg), `models/` (одна
`ggml-*.bin`), скрипты запуска, HTTP-сервер `voice2text-server` и
`README.md` для конечного пользователя (это
[README.dist.md](README.dist.md)).

## Сервер

[server/](server/) — Go без зависимостей, кросс-компилируется в `build.sh`
под все пять целей (macOS через `lipo` в универсальный бинарник). Отдаёт
OpenAI-совместимый `POST /v1/audio/transcriptions`: принимает multipart,
гонит файл через ffmpeg и whisper-cli из `bin/` рядом с собой, отдаёт
`json`, `text`, `srt`, `vtt` или `verbose_json`. Флаг `-emulate-429`
(`every:N`, `percent:P`, `first:N`, `always`) заставляет его отвечать
429 в формате OpenAI с `Retry-After` (флаг `-retry-after`), чтобы
проверять ретраи у клиентов. Логика выбора запросов детерминированная,
покрыта тестами, `build.sh` их прогоняет перед сборкой.

## Сборка

```bash
./build.sh                 # все платформы
./build.sh windows-x64     # одна
MODEL=ggml-small-q5_1.bin ./build.sh   # с другой моделью
```

Нужны: macOS с Xcode Command Line Tools, cmake, gh (для скачивания
релизов с GitHub), curl, zip. Всё скачанное и собранное лежит в `.cache/`,
повторные сборки ничего не качают. Результат в `dist/`.

Версии закреплены в начале `build.sh`: тег whisper.cpp, номер его
пре-релиза с бинарниками (у whisper.cpp бинарники прикреплены к
пре-релизам `bNNNN`, а не к тегам версий) и тег ffmpeg-static.

## Как устроен запуск

`launcher/transcribe.sh` и `launcher/transcribe.bat` делают одно и то же:
ffmpeg → 16 кГц моно WAV → whisper-cli → `.txt` и `.srt` рядом с
исходником. Особенности платформ, которые пришлось учесть:

- **macOS**: бинарники без подписи Apple, поэтому скрипт снимает
  карантин Gatekeeper (`xattr -dr com.apple.quarantine`). ffmpeg два,
  скрипт выбирает по `uname -m`.
- **Linux**: whisper-cli слинкован динамически с `libwhisper.so` и
  вариантами `libggml-cpu-*.so`, все лежат в `bin/`, скрипт выставляет
  `LD_LIBRARY_PATH`. Ещё ему нужна `libgomp.so.1`, которой в минимальных
  системах нет, поэтому она берётся из пакета `libgomp1` Ubuntu 22.04 и
  тоже кладётся в `bin/`.
- **Windows**: whisper-cli собран MSVC и требует VC++ runtime, скрипт
  ставит его с aka.ms, если нет. Пути с кириллицей не доходят до
  whisper-cli целыми, поэтому он работает с ASCII-путём в `%TEMP%`, а
  результат переносится `move`. Сам `.bat` только ASCII и с CRLF.

## Что проверено

| Пакет | Проверка |
|---|---|
| macos | arm64 на этой машине, с выставленным карантином, Metal включается. x86_64-срез не запускался (нет Rosetta). Сервер: curl и официальный SDK OpenAI, включая его ретраи на имитируемые 429 |
| linux-arm64 | минимальный Debian 12 в Docker: transcribe.sh и сервер с `-emulate-429 every:2` |
| linux-x64 | не запускался (не было x64-образа), бинарники те же, что arm64, плюс варианты CPU |
| windows-x64, windows-arm64 | не запускались, нужна Windows-машина |

## Выпуск

```bash
./build.sh
gh release create v$(date +%Y%m%d) dist/*.zip --title "voice2text $(date +%Y%m%d)"
```
