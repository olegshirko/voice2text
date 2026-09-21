# voice2text

Офлайн-распознавание речи из видео и аудио. Ничего устанавливать не
нужно. Входной файл может быть любым, который открывает ffmpeg: mp4, mkv,
mov, webm, mp3, m4a, wav, ogg. Рядом с ним появятся `<имя>.txt` и
`<имя>.srt`.

## Запуск

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
- **Linux.** Нужна glibc 2.35 или новее: Ubuntu 22.04, Debian 12 и всё,
  что вышло позже.

## HTTP-сервер

Сервер реализует `POST /v1/audio/transcriptions` в формате OpenAI Audio
API. Любой клиент OpenAI работает с ним после замены base URL.

```bash
./serve.sh                       # macOS, Linux
serve.bat                        # Windows
./serve.sh -addr 0.0.0.0:9000    # слушать на всех интерфейсах
./serve.sh -h                    # все флаги
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

Поля запроса: `file` (обязательное), `language` (по умолчанию `ru`),
`response_format` (`json`, `text`, `srt`, `vtt`, `verbose_json`),
`prompt`. Поле `model` принимается и игнорируется. Также доступны
`GET /health` и `GET /v1/models`.

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

`/health` и `/v1/models` под имитацию не попадают.

## Требования

- Оперативная память: 1,5 ГБ свободной.
- CPU: x86-64 с SSE4.2 или ARM64. GPU не нужен, на Apple Silicon
  используется Metal.

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
https://huggingface.co/ggerganov/whisper.cpp/tree/main, скрипты берут
первый найденный `ggml-*.bin`. Для слабых машин подойдёт
`ggml-small-q5_1.bin` (заметно хуже на русском), для максимальной
точности `ggml-large-v3-q5_0.bin` (1,1 ГБ, примерно в 3 раза медленнее
turbo).

## Лицензии

whisper.cpp (MIT), веса Whisper (MIT), ffmpeg (GPL), libgomp (GPL с
runtime exception). Подробнее в `licenses/`.
