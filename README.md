# voice2text

Офлайн-распознавание речи из видео и аудио для macOS, Linux и Windows.
Один архив на платформу: распаковать и запустить, ничего устанавливать
не нужно. Внутри [whisper.cpp](https://github.com/ggml-org/whisper.cpp),
модель Whisper large-v3-turbo, ffmpeg и HTTP-сервер с API, совместимым
с OpenAI.

## Скачать

[Releases](https://github.com/olegshirko/voice2text/releases/latest)

| Архив | Платформа |
|---|---|
| `voice2text-macos-*.zip` | macOS 12+, Intel и Apple Silicon |
| `voice2text-linux-x64-*.zip` | Linux x86-64, glibc 2.35+ (Ubuntu 22.04, Debian 12 и новее) |
| `voice2text-linux-arm64-*.zip` | Linux ARM64, glibc 2.35+ |
| `voice2text-windows-x64-*.zip` | Windows 10/11 x64 |
| `voice2text-windows-arm64-*.zip` | Windows 11 ARM64 |

Каждый архив около 570 МБ, из них 550 МБ занимает модель.

## Использование

Входной файл может быть любым, который открывает ffmpeg: mp4, mkv, mov,
webm, mp3, m4a, wav, ogg. Рядом с ним появятся `<имя>.txt` и `<имя>.srt`.

macOS и Linux:

```bash
./transcribe.sh lecture.mp4        # русский
./transcribe.sh lecture.mp4 en     # другой язык, auto для автоопределения
```

Windows: перетащить файл на `transcribe.bat` или из командной строки:

```bat
transcribe.bat "C:\video\lecture.mp4"
transcribe.bat "C:\video\lecture.mp4" en
```

### Первый запуск

- **macOS.** Бинарники не подписаны Apple. Скрипт сам снимает с них
  карантин, поэтому запускать нужно из терминала, а не двойным кликом.
- **Windows.** SmartScreen может спросить про неизвестное приложение:
  «Подробнее», затем «Выполнить в любом случае». Если в системе нет
  Visual C++ Runtime, скрипт скачает его с сайта Microsoft и попросит
  права администратора. Это происходит один раз.
- **Linux.** Дополнительных пакетов не требуется.

## HTTP-сервер

Сервер реализует `POST /v1/audio/transcriptions` в формате OpenAI Audio
API. Любой клиент OpenAI работает с ним после замены base URL.

```bash
./serve.sh                       # macOS, Linux
serve.bat                        # Windows
./serve.sh -addr 0.0.0.0:9000    # слушать на всех интерфейсах
```

```bash
curl http://127.0.0.1:8080/v1/audio/transcriptions \
  -F file=@lecture.mp4 -F language=ru
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="local")
result = client.audio.transcriptions.create(model="whisper-1", file=open("lecture.mp4", "rb"))
print(result.text)
```

Поля запроса:

| Поле | Значение |
|---|---|
| `file` | обязательное, любой формат |
| `language` | код языка, по умолчанию `ru` |
| `response_format` | `json` (по умолчанию), `text`, `srt`, `vtt`, `verbose_json` |
| `prompt` | подсказка для модели |
| `model` | принимается и игнорируется |

Также доступны `GET /health` и `GET /v1/models`.

Флаги сервера:

| Флаг | По умолчанию | Назначение |
|---|---|---|
| `-addr` | `127.0.0.1:8080` | адрес и порт |
| `-lang` | `ru` | язык, если клиент не передал `language` |
| `-threads` | число ядер | потоков whisper-cli |
| `-concurrency` | `1` | сколько запросов распознаются одновременно |
| `-model` | первая `ggml-*.bin` в `models/` | путь к модели |
| `-emulate-429` | выключено | имитация rate limit, см. ниже |
| `-retry-after` | `20` | значение `Retry-After` для имитируемых 429 |

### Имитация 429

Для проверки ретраев у клиентов сервер может отвечать
`429 Too Many Requests` в формате OpenAI: заголовки `Retry-After` и
`x-ratelimit-*`, тело с `"type": "rate_limit_error"`.

```bash
./serve.sh -emulate-429 every:3      # каждый третий запрос
./serve.sh -emulate-429 percent:30   # первые 30 из каждых 100
./serve.sh -emulate-429 first:5      # первые пять, дальше без ограничений
./serve.sh -emulate-429 always       # все
./serve.sh -emulate-429 every:2 -retry-after 60
```

Отклонённые запросы не доходят до распознавания. `/health` и
`/v1/models` под имитацию не попадают.

## Требования

- Оперативная память: 1,5 ГБ свободной.
- CPU: x86-64 с SSE4.2 или ARM64. Оптимизации под AVX2, AVX-512 и Zen4
  выбираются автоматически.
- GPU не нужен. На Apple Silicon используется Metal.


## Скорость

Замер на 96 секундах речи, модель по умолчанию `large-v3-turbo-q5_0`:

| Машина | Время | Час записи |
|---|---|---|
| Apple M5 Pro, Metal | 2 с | около 1 минуты |
| Intel N150, 4 ядра (слабый мини-ПК) | 196 с | около 2 часов |

На слабых процессорах модель по умолчанию работает медленнее реального
времени. Для них стоит взять модель поменьше, на том же Intel N150:

| Модель | Размер | 96 с речи | Час записи |
|---|---|---|---|
| `ggml-base-q5_1.bin` | 60 МБ | 12 с | 8 минут |
| `ggml-small-q5_1.bin` | 190 МБ | 46 с | 30 минут |
| `ggml-large-v3-turbo-q5_0.bin` | 550 МБ | 196 с | 2 часа |

Обычный ноутбук или десктоп последних лет находится между этими двумя
машинами, ближе к N150 без GPU и ближе к M5 Pro с ним.

## Модель

В `models/` лежит `ggml-large-v3-turbo-q5_0.bin`. Её можно заменить
любой другой из
[ggerganov/whisper.cpp](https://huggingface.co/ggerganov/whisper.cpp/tree/main),
скрипты берут первый найденный `ggml-*.bin`:

| Модель | Размер | Когда брать |
|---|---|---|
| `ggml-small-q5_1.bin` | 190 МБ | слабые машины, заметно хуже на русском |
| `ggml-large-v3-turbo-q5_0.bin` | 550 МБ | по умолчанию |
| `ggml-large-v3-q5_0.bin` | 1,1 ГБ | максимальная точность, примерно в 3 раза медленнее turbo |

## Сборка из исходников

Сборка выполняется на macOS. Нужны Xcode Command Line Tools, cmake, Go,
gh, curl, zip.

```bash
./build.sh                             # все платформы
./build.sh windows-x64                 # одна
MODEL=ggml-small-q5_1.bin ./build.sh   # с другой моделью
```

Скрипт скачивает бинарники whisper.cpp из его релизов, статический
ffmpeg, модель с Hugging Face и libgomp из пакетов Ubuntu, собирает
whisper-cli для macOS из исходников (универсальный бинарник с Metal),
кросс-компилирует сервер из [server/](server/) и складывает архивы в
`dist/`. Загрузки кэшируются в `.cache/`. Версии зависимостей
закреплены в начале `build.sh`.

Выпуск:

```bash
gh release create v$(date +%Y%m%d) dist/*.zip
```

## Состояние

| Платформа | Проверка |
|---|---|
| macOS Apple Silicon | распознавание и сервер, включая ретраи OpenAI SDK на имитируемые 429 |
| Linux x64 | распознавание и сервер на Ubuntu 25.10, Intel N150 |
| Linux ARM64 | распознавание и сервер в Debian 12 |
| macOS Intel, Windows | собраны из тех же официальных бинарников, на реальных машинах не запускались |

## Лицензии

Код репозитория: MIT. Компоненты в архивах: whisper.cpp (MIT), веса
Whisper (MIT), ffmpeg (GPL), libgomp (GPL с runtime exception). Подробнее
в [licenses/](licenses/).
