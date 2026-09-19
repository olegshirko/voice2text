# voice2text

Распознавание речи из видео и аудио. Работает полностью офлайн, без
установки чего-либо: whisper.cpp + модель Whisper large-v3-turbo + ffmpeg.

Понимает всё, что открывает ffmpeg: mp4, mkv, mov, webm, avi, mp3, m4a,
wav, ogg и так далее. Рядом с исходным файлом появятся:

- `<имя>.txt` — чистый текст;
- `<имя>.srt` — субтитры с таймкодами.

## Windows

Перетащите файл на `transcribe.bat`. Или из командной строки:

    transcribe.bat "C:\видео\lecture.mp4"
    transcribe.bat "C:\видео\lecture.mp4" en

При первом запуске Windows SmartScreen может спросить, запускать ли
неизвестное приложение: «Подробнее» → «Выполнить в любом случае».
Если на компьютере нет Visual C++ runtime, скрипт скачает его с сайта
Microsoft и попросит права администратора на установку. Это один раз.

## macOS

    ./transcribe.sh ~/Downloads/lecture.mp4
    ./transcribe.sh ~/Downloads/lecture.mp4 en

Бинарники не подписаны сертификатом Apple, скрипт сам снимает с них
карантин. Работает на Intel и Apple Silicon (на Apple Silicon считает на
GPU через Metal).

## Linux

    ./transcribe.sh ~/Downloads/lecture.mp4
    ./transcribe.sh ~/Downloads/lecture.mp4 en

Нужна glibc 2.35 или новее (Ubuntu 22.04, Debian 12, Fedora 36 и всё, что
вышло позже).

## HTTP-сервер (REST API)

В пакете есть сервер, совместимый с OpenAI Audio API. Любой клиент,
умеющий работать с `https://api.openai.com/v1/audio/transcriptions`,
работает и с ним, достаточно подменить base URL.

    ./serve.sh                       # macOS, Linux: слушает 127.0.0.1:8080
    serve.bat                        # Windows
    ./serve.sh -addr 0.0.0.0:9000    # открыть наружу, другой порт
    ./serve.sh -h                    # все флаги

Запрос:

    curl http://127.0.0.1:8080/v1/audio/transcriptions \
      -F file=@lecture.mp4 -F model=whisper-1 -F language=ru

    {"text":"..."}

Поля формы: `file` (обязательно, любой формат), `language` (по умолчанию
`ru`, флаг `-lang`), `response_format` (`json`, `text`, `srt`, `vtt`,
`verbose_json` с сегментами и таймкодами), `prompt`. Поле `model` и
заголовок `Authorization` принимаются и игнорируются. Ещё есть
`GET /health` и `GET /v1/models`.

Пример с официальным SDK:

    from openai import OpenAI
    client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="local")
    r = client.audio.transcriptions.create(model="whisper-1", file=open("lecture.mp4", "rb"))
    print(r.text)

Запросы обрабатываются по очереди (флаг `-concurrency`), потоков
whisper-cli по числу ядер (флаг `-threads`).

### Имитация 429 Too Many Requests

Чтобы проверить, как ваш клиент переживает rate limit, сервер умеет
отвечать 429 ровно так, как это делает OpenAI: заголовок `Retry-After`,
заголовки `x-ratelimit-*` и тело
`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded",...}}`.

    ./serve.sh -emulate-429 every:3      # каждый третий запрос
    ./serve.sh -emulate-429 percent:30   # 30 из каждых 100 запросов (первые 30)
    ./serve.sh -emulate-429 first:5      # первые пять запросов, дальше нормально
    ./serve.sh -emulate-429 always       # все
    ./serve.sh -emulate-429 every:2 -retry-after 60   # Retry-After: 60 (по умолчанию 20)

Отклонённые запросы не доходят до распознавания и считаются по счётчику
запросов на `/v1/audio/transcriptions`; `/health` и `/v1/models` под
имитацию не попадают.

## Язык

Второй аргумент — код языка: `ru` (по умолчанию), `en`, `de`, `fr`, `uk`,
… или `auto` для автоопределения. Явно указанный язык точнее, чем `auto`.

## Скорость и требования

- Оперативная память: около 1,5 ГБ свободной.
- CPU: любой x86-64 с SSE4.2 (примерно с 2010 года) или ARM64. Сборка
  сама выбирает вариант под процессор: AVX2, AVX-512, Zen4 и так далее.
- Скорость на обычном ноутбуке: час записи за 10–30 минут. На Apple
  Silicon и свежих десктопах — за 2–5 минут.

## Другая модель

В папке `models/` лежит одна модель. Её можно заменить любой другой из
https://huggingface.co/ggerganov/whisper.cpp/tree/main (файл `ggml-*.bin`),
скрипт возьмёт первую найденную. Для слабых машин подойдёт
`ggml-small-q5_1.bin` (190 МБ, в 3–4 раза быстрее, заметно хуже на
русском), для максимальной точности — `ggml-large-v3-q5_0.bin`
(1,1 ГБ, в 3 раза медленнее).
