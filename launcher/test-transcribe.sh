#!/bin/sh
# Проверяет резолв модели в transcribe.sh на пакете-скелете с заглушками
# вместо ffmpeg и whisper-cli. Реальные бинарники и модель не нужны.
#
#   sh launcher/test-transcribe.sh
set -u

repo=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin" "$tmp/models"
: > "$tmp/models/ggml-small-test.bin"
: > "$tmp/models/ggml-large-test.bin"

# Заглушка ffmpeg: создаёт файл на месте последнего аргумента.
cat > "$tmp/bin/ffmpeg" <<'EOF'
#!/bin/sh
for out; do :; done
printf 'RIFF' > "$out"
EOF

# Заглушка whisper-cli: пишет свои аргументы в $ARGS_FILE.
cat > "$tmp/bin/whisper-cli" <<'EOF'
#!/bin/sh
{ for a in "$@"; do printf '%s\n' "$a"; done; } >> "$ARGS_FILE"
EOF
chmod +x "$tmp/bin/ffmpeg" "$tmp/bin/whisper-cli"
# macOS-сборка использует bin/ffmpeg-arm64, остальные — bin/ffmpeg.
cp "$tmp/bin/ffmpeg" "$tmp/bin/ffmpeg-arm64"

: > "$tmp/in.mp4"
cp "$repo/launcher/transcribe.sh" "$tmp/transcribe.sh"

failed=0
check() { # name want_bin_substring VOICE2TEXT_MODEL
  name=$1; want=$2; vtm=$3
  : > "$tmp/args.txt"
  if [ -n "$vtm" ]; then
    VOICE2TEXT_MODEL=$vtm ARGS_FILE=$tmp/args.txt sh "$tmp/transcribe.sh" "$tmp/in.mp4" en > "$tmp/out.txt" 2>&1
  else
    unset VOICE2TEXT_MODEL
    ARGS_FILE=$tmp/args.txt sh "$tmp/transcribe.sh" "$tmp/in.mp4" en > "$tmp/out.txt" 2>&1
  fi || { echo "FAIL $name: transcribe.sh завершился с ошибкой"; cat "$tmp/out.txt"; failed=1; return; }
  got=$(grep -o 'ggml[^ ]*\.bin' "$tmp/args.txt" | head -1)
  case $got in
    *"$want"*) echo "ok $name" ;;
    *) echo "FAIL $name: want $want, got '$got'"; cat "$tmp/out.txt"; failed=1 ;;
  esac
}

check default large-test ''
check by-name small-test small
check by-pattern small-test small-test
check by-path small-test "$tmp/models/ggml-small-test.bin"

# Несуществующее имя — ненулевой код и подсказка про models/.
: > "$tmp/args.txt"
if VOICE2TEXT_MODEL=nonexistent ARGS_FILE=$tmp/args.txt sh "$tmp/transcribe.sh" "$tmp/in.mp4" en > "$tmp/out.txt" 2>&1; then
  echo "FAIL nonexistent: ожидалась ошибка, вышло 0"
  failed=1
else
  case $(cat "$tmp/out.txt") in
    *models*) echo "ok nonexistent" ;;
    *) echo "FAIL nonexistent: в сообщении нет models/:"; cat "$tmp/out.txt"; failed=1 ;;
  esac
fi

if [ "$failed" -ne 0 ]; then echo "FAILED"; exit 1; fi
echo "all passed"
