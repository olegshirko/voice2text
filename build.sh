#!/bin/sh
# Собирает автономные пакеты voice2text для macOS, Linux и Windows в dist/.
#
#   ./build.sh            все платформы
#   ./build.sh linux-x64  одна платформа
#
# Скачанное и собранное кэшируется в .cache/, повторный запуск ничего не
# качает заново. Запускается на macOS: только macOS-бинарник собирается из
# исходников, остальное берётся из официальных релизов whisper.cpp.
set -eu

WHISPER_TAG=v1.9.4
WHISPER_BUILD=b5130          # пре-релиз whisper.cpp с бинарниками для этого тега
FFMPEG_STATIC_TAG=b6.1.1     # eugeneware/ffmpeg-static
MODEL=${MODEL:-ggml-large-v3-turbo-q5_0.bin}
VERSION=${VERSION:-$(date +%Y%m%d)}

here=$(cd "$(dirname "$0")" && pwd)
cache=$here/.cache
dist=$here/dist
mkdir -p "$cache/whisper" "$cache/ffmpeg" "$cache/models" "$cache/server" "$dist"

log() { printf '\033[1m== %s\033[0m\n' "$*"; }

fetch_release() { # repo tag asset dir
  [ -f "$4/$3" ] || { log "download $3"; gh release download "$2" --repo "$1" -p "$3" -D "$4"; }
}

fetch_model() {
  [ -f "$cache/models/$MODEL" ] || {
    log "download $MODEL"
    curl -L --fail -o "$cache/models/$MODEL.part" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/$MODEL"
    mv "$cache/models/$MODEL.part" "$cache/models/$MODEL"
  }
}

fetch_ffmpeg() { # name (ffmpeg-darwin-arm64 ...)
  [ -f "$cache/ffmpeg/$1" ] || {
    fetch_release eugeneware/ffmpeg-static $FFMPEG_STATIC_TAG "$1.gz" "$cache/ffmpeg"
    gunzip -k "$cache/ffmpeg/$1.gz"
    chmod +x "$cache/ffmpeg/$1"
  }
}

# libgomp.so.1 (OpenMP) нужна ubuntu-сборкам whisper.cpp, а в системе её может не быть.
fetch_libgomp() { # arch: amd64|arm64
  [ -f "$cache/libgomp/$1/libgomp.so.1" ] && return
  log "download libgomp1 ($1)"
  deb=libgomp1_12.3.0-1ubuntu1~22.04.3_$1.deb
  case $1 in
    amd64) url=http://archive.ubuntu.com/ubuntu/pool/main/g/gcc-12/$deb ;;
    arm64) url=http://ports.ubuntu.com/ubuntu-ports/pool/main/g/gcc-12/$deb ;;
  esac
  d=$cache/libgomp/$1
  rm -rf "$d"; mkdir -p "$d"
  curl -sS -L --fail -o "$d/$deb" "$url"
  (cd "$d" && ar x "$deb" && tar xf data.tar.* && cp usr/lib/*/libgomp.so.1 . && rm -rf usr data.tar.* control.tar.* debian-binary)
}

# HTTP-сервер на Go, кросс-компилируется под все платформы.
build_server() { # GOOS GOARCH out
  [ -f "$3" ] && return
  log "build voice2text-server $1/$2"
  (cd "$here/server" && CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags '-s -w' -o "$3" .)
}
server_bin() { # GOOS GOARCH -> путь
  echo "$cache/server/voice2text-server-$1-$2"
}

build_macos_whisper() {
  bin=$cache/whisper.cpp/build/bin/whisper-cli
  [ -f "$bin" ] && return
  log "build whisper-cli for macOS (universal)"
  [ -d "$cache/whisper.cpp" ] || git clone --depth 1 -b $WHISPER_TAG https://github.com/ggml-org/whisper.cpp.git "$cache/whisper.cpp"
  cmake -S "$cache/whisper.cpp" -B "$cache/whisper.cpp/build" \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
    -DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_NATIVE=OFF \
    -DCMAKE_OSX_ARCHITECTURES="arm64;x86_64" -DCMAKE_OSX_DEPLOYMENT_TARGET=12.0 \
    -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF -DWHISPER_SDL2=OFF
  cmake --build "$cache/whisper.cpp/build" --config Release --target whisper-cli -j
}

# Общий скелет пакета: модель, README, лицензии.
stage() { # platform -> печатает путь к каталогу пакета
  name=voice2text-$1
  d=$dist/$name
  rm -rf "$d"
  mkdir -p "$d/bin" "$d/models" "$d/licenses"
  ln "$cache/models/$MODEL" "$d/models/$MODEL" 2>/dev/null || cp "$cache/models/$MODEL" "$d/models/$MODEL"
  cp "$here/README.dist.md" "$d/README.md"
  cp "$here/licenses/"* "$d/licenses/"
  echo "$d"
}

pack() { # dir
  log "pack $(basename "$1")"
  (cd "$dist" && rm -f "$(basename "$1")-$VERSION.zip" && zip -qr "$(basename "$1")-$VERSION.zip" "$(basename "$1")")
  rm -rf "$1"
}

do_macos() {
  build_macos_whisper
  fetch_ffmpeg ffmpeg-darwin-arm64
  fetch_ffmpeg ffmpeg-darwin-x64
  d=$(stage macos)
  cp "$cache/whisper.cpp/build/bin/whisper-cli" "$d/bin/"
  cp "$cache/ffmpeg/ffmpeg-darwin-arm64" "$d/bin/ffmpeg-arm64"
  cp "$cache/ffmpeg/ffmpeg-darwin-x64" "$d/bin/ffmpeg-x64"
  cp "$here/launcher/transcribe.sh" "$here/launcher/serve.sh" "$d/"
  build_server darwin arm64 "$(server_bin darwin arm64)"
  build_server darwin amd64 "$(server_bin darwin amd64)"
  lipo -create -output "$d/voice2text-server" "$(server_bin darwin arm64)" "$(server_bin darwin amd64)"
  pack "$d"
}

do_linux() { # arch: x64|arm64
  asset=whisper-bin-ubuntu-$1.tar.gz
  fetch_release ggml-org/whisper.cpp $WHISPER_BUILD "$asset" "$cache/whisper"
  fetch_ffmpeg "ffmpeg-linux-$1"
  case $1 in x64) deb_arch=amd64 ;; *) deb_arch=$1 ;; esac
  fetch_libgomp "$deb_arch"
  d=$(stage "linux-$1")
  tmp=$(mktemp -d)
  tar xzf "$cache/whisper/$asset" -C "$tmp"
  rm -f "$tmp"/*/libparakeet*
  cp "$tmp"/*/whisper-cli "$tmp"/*/*.so* "$d/bin/"
  cp "$cache/libgomp/$deb_arch/libgomp.so.1" "$d/bin/"
  rm -rf "$tmp"
  cp "$cache/ffmpeg/ffmpeg-linux-$1" "$d/bin/ffmpeg"
  cp "$here/launcher/transcribe.sh" "$here/launcher/serve.sh" "$d/"
  case $1 in x64) goarch=amd64 ;; *) goarch=$1 ;; esac
  build_server linux $goarch "$(server_bin linux $goarch)"
  cp "$(server_bin linux $goarch)" "$d/voice2text-server"
  pack "$d"
}

do_windows() { # arch: x64|arm64
  case $1 in
    x64)   asset=whisper-bin-x64.zip ;;
    arm64) asset=whisper-bin-win-cpu-arm64.zip ;;
  esac
  fetch_release ggml-org/whisper.cpp $WHISPER_BUILD "$asset" "$cache/whisper"
  d=$(stage "windows-$1")
  tmp=$(mktemp -d)
  unzip -q "$cache/whisper/$asset" -d "$tmp"
  cp "$tmp"/Release/whisper-cli.exe "$tmp"/Release/whisper.dll "$tmp"/Release/ggml*.dll "$d/bin/"
  cp "$tmp"/Release/libomp*.dll "$d/bin/" 2>/dev/null || true
  rm -rf "$tmp"
  case $1 in
    x64)
      fetch_ffmpeg ffmpeg-win32-x64
      cp "$cache/ffmpeg/ffmpeg-win32-x64" "$d/bin/ffmpeg.exe"
      ;;
    arm64)
      # ffmpeg-static не собирает под Windows ARM64, берём BtbN.
      asset=ffmpeg-master-latest-winarm64-gpl.zip
      fetch_release BtbN/FFmpeg-Builds latest "$asset" "$cache/ffmpeg"
      unzip -qj "$cache/ffmpeg/$asset" '*/bin/ffmpeg.exe' -d "$d/bin/"
      ;;
  esac
  # CRLF, чтобы cmd.exe не спотыкался, и BOM не нужен: в файле только ASCII.
  sed 's/$/\r/' "$here/launcher/transcribe.bat" > "$d/transcribe.bat"
  sed 's/$/\r/' "$here/launcher/serve.bat" > "$d/serve.bat"
  case $1 in x64) goarch=amd64 ;; *) goarch=$1 ;; esac
  build_server windows $goarch "$(server_bin windows $goarch).exe"
  cp "$(server_bin windows $goarch).exe" "$d/voice2text-server.exe"
  pack "$d"
}

fetch_model
# Сервер пересобирается при каждом запуске: это секунды, а исходники могли измениться.
rm -rf "$cache/server"; mkdir -p "$cache/server"
(cd "$here/server" && go test ./... >/dev/null) || { echo "server tests failed" >&2; exit 1; }
targets=${*:-"macos linux-x64 linux-arm64 windows-x64 windows-arm64"}
for t in $targets; do
  case $t in
    macos)         do_macos ;;
    linux-x64)     do_linux x64 ;;
    linux-arm64)   do_linux arm64 ;;
    windows-x64)   do_windows x64 ;;
    windows-arm64) do_windows arm64 ;;
    *) echo "unknown target: $t" >&2; exit 1 ;;
  esac
done
log "done"
ls -la "$dist"/*.zip
