#!/bin/sh
# voice2text — запуск HTTP-сервера (OpenAI-совместимый POST /v1/audio/transcriptions).
#
#   ./serve.sh                         на 127.0.0.1:8080
#   ./serve.sh -addr 0.0.0.0:9000      на всех интерфейсах
#   ./serve.sh -emulate-429 every:3    каждый третий запрос получает 429
#   ./serve.sh -h                      все флаги
here=$(cd "$(dirname "$0")" && pwd)
if [ "$(uname -s)" = Darwin ]; then
  xattr -dr com.apple.quarantine "$here" 2>/dev/null || true
fi
chmod +x "$here"/bin/* "$here/voice2text-server" 2>/dev/null || true
exec "$here/voice2text-server" "$@"
