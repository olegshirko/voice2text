#!/bin/sh
# voice2text — распознавание речи из видео или аудио, полностью офлайн.
#
#   ./transcribe.sh <файл> [язык]
#
# Язык: ru (по умолчанию), en, de, ... или auto. Рядом с файлом появятся
# <имя>.txt и <имя>.srt.
set -e

here=$(cd "$(dirname "$0")" && pwd)

if [ -z "$1" ]; then
  echo "usage: $0 <video-or-audio-file> [lang]" >&2
  exit 1
fi
in=$1
lang=${2:-ru}
out=${in%.*}

os=$(uname -s)
arch=$(uname -m)
case "$os" in
  Darwin)
    # Снять карантин Gatekeeper с распакованных бинарников, иначе macOS их не запустит.
    xattr -dr com.apple.quarantine "$here" 2>/dev/null || true
    case "$arch" in arm64) ffmpeg="$here/bin/ffmpeg-arm64" ;; *) ffmpeg="$here/bin/ffmpeg-x64" ;; esac
    threads=$(sysctl -n hw.ncpu)
    ;;
  *)
    ffmpeg="$here/bin/ffmpeg"
    threads=$(nproc 2>/dev/null || echo 4)
    export LD_LIBRARY_PATH="$here/bin${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    ;;
esac
chmod +x "$here"/bin/* 2>/dev/null || true

# Ни ffmpeg, ни whisper-cli не должны трогать stdin: иначе в цикле вида
# `find ... | while read f; do ./transcribe.sh "$f"; done` они съедают
# имена следующих файлов.

model=$(ls "$here"/models/ggml-*.bin 2>/dev/null | head -1)
if [ -z "$model" ]; then
  echo "no model found in $here/models" >&2
  exit 1
fi

tmp="${TMPDIR:-/tmp}/voice2text-$$.wav"
trap 'rm -f "$tmp"' EXIT

echo "[1/2] extracting audio..."
"$ffmpeg" -nostdin -v error -y -i "$in" -vn -ac 1 -ar 16000 -c:a pcm_s16le "$tmp"

echo "[2/2] transcribing with $(basename "$model"), lang=$lang, threads=$threads..."
"$here/bin/whisper-cli" -m "$model" -l "$lang" -t "$threads" -f "$tmp" \
  -otxt -osrt -of "$out" -np -pp </dev/null

echo
echo "done: $out.txt, $out.srt"
