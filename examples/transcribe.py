#!/usr/bin/env python3
# Клиент voice2text на чистом urllib: без зависимостей, только стандартная
# библиотека. Показывает полный набор навыков работы с HTTP API:
#
#   python3 transcribe.py http://127.0.0.1:8080 lecture.mp4 [ru]
#
# - загрузка файла multipart/form-data
# - ретраи на 429 (Retry-After + экспоненциальный backoff, максимум 5 попыток)
# - отказ ретраить 500 (серверная ошибка не лечится повтором)
# - таймаут на весь запрос
# - валидация тела ответа: даже при 200 JSON может оказаться битым
#   (сервер умеет это имитировать: ./serve.sh -emulate-broken-json always)

import json
import sys
import time
import urllib.error
import urllib.request
import uuid

MAX_ATTEMPTS = 5


def fail(msg):
    print(f"ошибка: {msg}", file=sys.stderr)
    sys.exit(1)


def main():
    if len(sys.argv) < 3:
        print(f"usage: {sys.argv[0]} <server-url> <file> [lang]", file=sys.stderr)
        sys.exit(1)
    base, path = sys.argv[1].rstrip("/"), sys.argv[2]
    lang = sys.argv[3] if len(sys.argv) > 3 else "ru"

    try:
        audio = open(path, "rb").read()
    except OSError as e:
        fail(f"не могу прочитать {path}: {e}")

    # Границу делаем с uuid: устойчиво к любому содержимому файла.
    boundary = uuid.uuid4().hex
    body = (
        f'--{boundary}\r\n'
        f'Content-Disposition: form-data; name="file"; filename="{path}"\r\n'
        f"Content-Type: application/octet-stream\r\n\r\n"
    ).encode() + audio + f"\r\n--{boundary}\r\n".encode() + (
        f'Content-Disposition: form-data; name="language"\r\n\r\n{lang}\r\n'
        f"--{boundary}--\r\n"
    ).encode()

    for attempt in range(1, MAX_ATTEMPTS + 1):
        req = urllib.request.Request(
            f"{base}/v1/audio/transcriptions",
            data=body,
            headers={
                "Content-Type": f"multipart/form-data; boundary={boundary}",
                "X-Request-Id": uuid.uuid4().hex[:8],  # сервер вернёт его в ответе
            },
            method="POST",
        )
        try:
            # timeout на весь запрос: без него зависший сервер подвесит клиента.
            with urllib.request.urlopen(req, timeout=300) as resp:
                raw = resp.read()
                req_id = resp.headers.get("X-Request-Id", "?")
        except urllib.error.HTTPError as e:
            if e.code == 429:
                # Rate limit: сервер сам сказал, сколько ждать (Retry-After).
                # Плюс экспоненциальный запас — ждём вместе с другими клиентами.
                wait = int(e.headers.get("Retry-After", "5")) + 2 ** attempt
                print(f"429, попытка {attempt}/{MAX_ATTEMPTS}, ждём {wait}с...", file=sys.stderr)
                time.sleep(wait)
                continue
            # 500 и прочие: повтор обычно бессмыслен, ломаемся сразу.
            try:
                err = json.loads(e.read())["error"]["message"]
            except Exception:
                err = f"HTTP {e.code}"
            fail(f"{err} (HTTP {e.code})")
        except urllib.error.URLError as e:
            fail(f"сервер недоступен: {e.reason}")

        print(f"X-Request-Id: {req_id}", file=sys.stderr)
        # Валидация тела: 200 не гарантирует корректный JSON.
        try:
            print(json.loads(raw)["text"])
        except (json.JSONDecodeError, KeyError):
            fail("сервер вернул 200 с некорректным JSON — проверьте -emulate-broken-json")
        return

    fail(f"исчерпаны {MAX_ATTEMPTS} попыток")


if __name__ == "__main__":
    main()
