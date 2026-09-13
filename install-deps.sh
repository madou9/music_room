#!/usr/bin/env bash
#
# install-deps.sh - Install host dependencies for the Music Room project.
#
# Usage:  bash install-deps.sh
#
# Checks what's already available first, then installs only what's missing — all
# without requiring sudo.  Only installs host-side tooling; everything that runs
# inside Docker (Go, Postgres, Mailpit) is deliberately skipped here.
#
# The actual bytes are stored on goinfre (/goinfre/$USER) since $HOME is
# usually small/quota-limited, but every tool still lives at its normal $HOME
# path via a symlink — so PATH, ANDROID_HOME, Make, IDEs etc. all keep working
# unchanged, in interactive shells and non-interactive ones (make, scripts) alike.
#
# Installed to $HOME (symlinked onto goinfre), when missing:
#   Java JDK 17      ~/.local/jdk-17
#   Flutter SDK      ~/flutter
#   Android SDK      ~/Android/Sdk
#   k6               ~/.local/bin/k6
#

set -euo pipefail

# ---- OS check ---------------------------------------------------------------
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "[ERROR] This script only supports Linux. Install dependencies manually on $(uname -s)." >&2
  exit 1
fi

# ---- Detect shell rc file ---------------------------------------------------
case "${SHELL##*/}" in
  zsh)  RCFILE="$HOME/.zshrc" ;;
  bash) RCFILE="$HOME/.bashrc" ;;
  *)    RCFILE="$HOME/.bashrc" ;;
esac

# ---- Colours ----------------------------------------------------------------
RED='\033[0;31m';    GREEN='\033[0;32m'
YELLOW='\033[1;33m'; BLUE='\033[0;34m'
BOLD='\033[1m';      NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
header() { echo -e "\n${BOLD}── $* ──${NC}"; }
skip()   { echo -e "${YELLOW}[SKIP]${NC}  $*"; }

# ---- Helpers ----------------------------------------------------------------
in_path() { command -v "$1" &>/dev/null; }

version_ge() {
  local IFS=.
  read -ra v1 <<< "$1"
  read -ra v2 <<< "$2"
  for i in 0 1 2; do
    local a="${v1[$i]:-0}" b="${v2[$i]:-0}"
    (( a > b )) && return 0
    (( a < b )) && return 1
  done
  return 0
}

ensure_dir() { mkdir -p "$1"; }

add_profile_line() {
  local marker="$1" line="$2"
  if ! grep -qF "$marker" "$RCFILE" 2>/dev/null; then
    echo "$line" >> "$RCFILE"
  fi
}

ensure_path() {
  local dir="$1"
  case ":$PATH:" in
    *":$dir:"*) ;;
    *) export PATH="$dir:$PATH" ;;
  esac
}

# Keeps $home_path at its normal, expected location while the actual bytes
# live on goinfre: moves a real (non-symlink) dir there once, then makes
# $home_path a symlink to it. A no-op once $home_path is already a symlink.
redirect_to_goinfre() {
  local home_path="$1" goinfre_path="$2"
  if [ -L "$home_path" ]; then
    # goinfre is local, per-machine scratch disk: it doesn't follow $HOME to
    # a different physical workstation, and can be cleared between sessions.
    # The symlink itself survives (it lives under $HOME) but its target can
    # go missing, leaving a dangling link. Recreate the target so it's not
    # pointing into a void — no need to touch the symlink itself.
    [ -d "$goinfre_path" ] || ensure_dir "$goinfre_path"
    return 0
  fi
  if [ -d "$home_path" ]; then
    if [ -d "$goinfre_path" ]; then
      warn "$goinfre_path already exists — leaving $home_path as a real directory"
      return 0
    fi
    info "Moving $home_path -> $goinfre_path"
    ensure_dir "$(dirname "$goinfre_path")"
    mv "$home_path" "$goinfre_path"
  fi
  [ -e "$home_path" ] && return 0
  ensure_dir "$goinfre_path"
  ensure_dir "$(dirname "$home_path")"
  ln -s "$goinfre_path" "$home_path"
}

# ---- Scratch disk (goinfre) --------------------------------------------------
header "Scratch disk"

if [ -d "/goinfre/$USER" ]; then
  GOINFRE_DIR="/goinfre/$USER"
elif [ -d "$HOME/goinfre" ]; then
  GOINFRE_DIR="$(cd "$HOME/goinfre" && pwd)"
else
  GOINFRE_DIR=""
  warn "No goinfre scratch disk found — installing straight to \$HOME."
fi

if [ -n "$GOINFRE_DIR" ]; then
  ok "Large installs will be stored on $GOINFRE_DIR, linked from \$HOME"
fi

# ---- Prerequisite check -----------------------------------------------------
header "Prerequisite check"

MISSING_TOOLS=()
for tool in curl wget unzip; do
  if in_path "$tool"; then
    ok "$tool found"
  else
    MISSING_TOOLS+=("$tool")
  fi
done

if [ ${#MISSING_TOOLS[@]} -gt 0 ]; then
  error "Missing essential tools: ${MISSING_TOOLS[*]}"
  error "Install them first (e.g. with your system package manager) and re-run."
  exit 1
fi

# ---- Docker (host) ----------------------------------------------------------
header "Docker & Docker Compose"

if in_path docker && docker compose version &>/dev/null; then
  DOCKER_VER=$(docker --version 2>/dev/null | grep -oP '\d+\.\d+\.\d+' | head -1 || echo "?")
  ok "Docker $DOCKER_VER + Compose plugin already installed"
else
  warn "Docker or docker compose plugin not detected."
  warn "Install Docker Desktop or the Docker engine, then re-run this script."
  warn "  https://docs.docker.com/engine/install/"
  warn ""
  warn "After installing, add your user to the docker group:"
  warn "  sudo usermod -aG docker $(whoami) && newgrp docker"
fi

# ---- Java JDK 17 (for Android builds) ---------------------------------------
header "Java JDK 17"

JAVA_HOME_DIR="$HOME/.local/jdk-17"
JAVA_BIN="$JAVA_HOME_DIR/bin/java"

if in_path java; then
  JAVA_VER=$(java -version 2>&1 | awk -F'"' '/version/ {print $2}' | cut -d. -f1)
  if [ "$JAVA_VER" -ge 17 ] 2>/dev/null; then
    ok "Java $JAVA_VER detected (>= 17) at $(command -v java)"
  else
    warn "Java $JAVA_VER is too old (need 17+) — installing JDK 17 locally"
    JAVA_INSTALL=yes
  fi
elif [ -x "$JAVA_BIN" ]; then
  JAVA_VER=$("$JAVA_BIN" -version 2>&1 | awk -F'"' '/version/ {print $2}' | cut -d. -f1)
  if [ "$JAVA_VER" -ge 17 ] 2>/dev/null; then
    ok "Java $JAVA_VER found at $JAVA_HOME_DIR"
    ensure_path "$JAVA_HOME_DIR/bin"
  else
    warn "Local JDK too old — reinstalling"
    rm -rf "$JAVA_HOME_DIR"
    JAVA_INSTALL=yes
  fi
else
  info "Java not found — installing JDK 17 to $JAVA_HOME_DIR"
  JAVA_INSTALL=yes
fi

if [ "${JAVA_INSTALL:-no}" = "yes" ]; then
  [ -n "$GOINFRE_DIR" ] && redirect_to_goinfre "$JAVA_HOME_DIR" "$GOINFRE_DIR/jdk-17"
  JDK_URL="https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.15%2B6/OpenJDK17U-jdk_x64_linux_hotspot_17.0.15_6.tar.gz"
  info "Downloading Eclipse Temurin JDK 17…"
  wget -q --show-progress "$JDK_URL" -O /tmp/jdk17.tar.gz
  tar -xzf /tmp/jdk17.tar.gz -C /tmp/
  rm -f /tmp/jdk17.tar.gz
  # The tarball extracts to a directory like jdk-17.0.15+6 — move contents
  JDK_EXTRACTED=$(find /tmp -maxdepth 1 -type d -name 'jdk-17*' | head -1)
  if [ -n "$JDK_EXTRACTED" ]; then
    rm -f "$JAVA_HOME_DIR" # drop a dangling symlink left by redirect_to_goinfre, if any
    ensure_dir "$(dirname "$JAVA_HOME_DIR")"
    mv "$JDK_EXTRACTED" "$JAVA_HOME_DIR"
  else
    error "JDK extraction failed — unexpected tarball structure"
    exit 1
  fi
  ok "JDK 17 installed to $JAVA_HOME_DIR"
  ensure_path "$JAVA_HOME_DIR/bin"
  add_profile_line "jdk-17" $'# Java JDK 17\nexport PATH="$PATH:$HOME/.local/jdk-17/bin"'
fi

# ---- Flutter SDK ------------------------------------------------------------
header "Flutter SDK"

FLUTTER_MIN="3.19.0"
FLUTTER_HOME="${FLUTTER_HOME:-$HOME/flutter}"
FLUTTER_BIN="$FLUTTER_HOME/bin/flutter"

[ -n "$GOINFRE_DIR" ] && redirect_to_goinfre "$FLUTTER_HOME" "$GOINFRE_DIR/flutter"

if in_path flutter; then
  FLUTTER_VER=$(flutter --version 2>/dev/null | grep -oP 'Flutter \K\d+\.\d+\.\d+' | head -1 || echo "0.0.0")
  if version_ge "$FLUTTER_VER" "$FLUTTER_MIN"; then
    ok "Flutter $FLUTTER_VER detected (>= $FLUTTER_MIN)"
  else
    warn "Flutter $FLUTTER_VER is too old (need >= $FLUTTER_MIN) — upgrading"
    FLUTTER_INSTALL=yes
  fi
elif [ -x "$FLUTTER_BIN" ]; then
  FLUTTER_VER=$("$FLUTTER_BIN" --version 2>/dev/null | grep -oP 'Flutter \K\d+\.\d+\.\d+' | head -1 || echo "0.0.0")
  if version_ge "$FLUTTER_VER" "$FLUTTER_MIN"; then
    ok "Flutter $FLUTTER_VER found in $FLUTTER_HOME"
    ensure_path "$FLUTTER_HOME/bin"
    add_profile_line "flutter" $'# Flutter SDK\nexport PATH="$PATH:$HOME/flutter/bin"'
  else
    warn "Flutter in $FLUTTER_HOME is too old ($FLUTTER_VER) — reinstalling"
    rm -f "$FLUTTER_HOME"
    rm -rf "$GOINFRE_DIR/flutter" 2>/dev/null || true
    FLUTTER_INSTALL=yes
  fi
else
  info "Flutter not found — installing to $FLUTTER_HOME"
  FLUTTER_INSTALL=yes
fi

if [ "${FLUTTER_INSTALL:-no}" = "yes" ]; then
  info "Downloading Flutter SDK (stable)…"
  wget -q --show-progress \
    "https://storage.googleapis.com/flutter_infra_release/releases/stable/linux/flutter_linux_3.44.0-stable.tar.xz" \
    -O /tmp/flutter.tar.xz
  rm -f "$FLUTTER_HOME" # drop a dangling symlink left by redirect_to_goinfre, if any
  EXTRACT_DIR="${GOINFRE_DIR:-$HOME}"
  ensure_dir "$EXTRACT_DIR"
  tar -xf /tmp/flutter.tar.xz -C "$EXTRACT_DIR"
  rm /tmp/flutter.tar.xz
  if [ -n "$GOINFRE_DIR" ]; then
    ensure_dir "$(dirname "$FLUTTER_HOME")"
    ln -sfn "$EXTRACT_DIR/flutter" "$FLUTTER_HOME"
  fi
  ok "Flutter installed to $FLUTTER_HOME"
  ensure_path "$FLUTTER_HOME/bin"
  add_profile_line "flutter" $'# Flutter SDK\nexport PATH="$PATH:$HOME/flutter/bin"'
fi

# Resolve the binary to invoke for the rest of this script, rather than
# relying on a PATH lookup that may not reflect what was just installed.
if in_path flutter; then
  FLUTTER_BIN="$(command -v flutter)"
fi

# Cache common Flutter artifacts (no iOS/Web — saves bandwidth)
if [ -x "$FLUTTER_BIN" ]; then
  "$FLUTTER_BIN" precache --no-ios --no-web 2>/dev/null || true
fi

# ---- Android SDK (command-line tools) ---------------------------------------
header "Android SDK (optional — needed for mobile builds)"

ANDROID_HOME="$HOME/Android/Sdk"
CMDLINE_BIN="$ANDROID_HOME/cmdline-tools/latest/bin"
SDKMANAGER="$CMDLINE_BIN/sdkmanager"

[ -n "$GOINFRE_DIR" ] && redirect_to_goinfre "$ANDROID_HOME" "$GOINFRE_DIR/Android/Sdk"

if [ -x "$SDKMANAGER" ]; then
  ok "Android cmdline-tools already installed at $ANDROID_HOME"
else
  info "Installing Android command-line tools to $ANDROID_HOME"
  ensure_dir "$ANDROID_HOME"

  CMDLINE_URL="https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip"
  wget -q --show-progress "$CMDLINE_URL" -O /tmp/cmdline-tools.zip
  unzip -qo /tmp/cmdline-tools.zip -d /tmp/cmdline-tools-tmp
  ensure_dir "$ANDROID_HOME/cmdline-tools"
  mv /tmp/cmdline-tools-tmp/cmdline-tools "$ANDROID_HOME/cmdline-tools/latest"
  rm -rf /tmp/cmdline-tools.zip /tmp/cmdline-tools-tmp
  ok "Android cmdline-tools installed"

  if [ -x "$SDKMANAGER" ]; then
    yes | "$SDKMANAGER" --sdk_root="$ANDROID_HOME" --licenses >/dev/null 2>&1 || true
    info "Android SDK licenses accepted"
  fi
fi

# Always wire up ANDROID_HOME/PATH (not just on a fresh install) so a machine
# that already had the SDK from a previous run still gets it in new shells.
if [ -x "$SDKMANAGER" ]; then
  export ANDROID_HOME
  ensure_path "$CMDLINE_BIN"
  ensure_path "$ANDROID_HOME/platform-tools"
  ensure_path "$ANDROID_HOME/emulator"
  add_profile_line "Android/Sdk" $'# Android SDK\nexport ANDROID_HOME="$HOME/Android/Sdk"\nexport PATH="$PATH:$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator"'
fi

# Install essential Android SDK components (idempotent via sdkmanager)
if [ -x "$SDKMANAGER" ]; then
  LATEST_PLATFORM=$("$SDKMANAGER" --list --sdk_root="$ANDROID_HOME" 2>/dev/null | grep -oP 'platforms;android-\K\d+' | sort -rn | head -1 || echo "35")
  # cmake;3.10.2 (bundled Ninja) — AGP's documented default CMake version for
  # native builds that don't pin one themselves (e.g. the jni plugin, pulled
  # in transitively). Without it, externalNativeBuild fails with CXX1416
  # "Could not find Ninja on PATH or in SDK CMake bin folders".
  #
  # platforms;android-36 + build-tools;28.0.3 are pinned as a fallback:
  # "latest" (above) can point at a version not yet mirrored on the repo
  # (e.g. android-37 at the time of writing), and sdkmanager treats a failed
  # package install as non-fatal — so without a known-good fallback, a bad
  # "latest" guess silently leaves zero platforms installed and `flutter
  # doctor` reports "No valid Android SDK platforms found".
  for pkg in "platform-tools" "platforms;android-${LATEST_PLATFORM}" "build-tools;${LATEST_PLATFORM}.0.0" "cmake;3.10.2.4988404" "platforms;android-36" "build-tools;28.0.3"; do
    if "$SDKMANAGER" --list --sdk_root="$ANDROID_HOME" 2>/dev/null | grep -q "Installed.*$pkg" 2>/dev/null; then
      ok "Android $pkg already installed"
    else
      info "Installing Android $pkg…"
      "$SDKMANAGER" --sdk_root="$ANDROID_HOME" "$pkg" >/dev/null 2>&1 || warn "Failed to install $pkg (non-fatal)"
    fi
  done
  ok "Android SDK components installed"
fi

# Gradle and the Dart/Flutter pub cache both default to dirs under $HOME that
# can grow to several GB (this is what starved the Android NDK auto-install
# of disk space) — redirect them onto goinfre the same way.
if [ -n "$GOINFRE_DIR" ]; then
  redirect_to_goinfre "$HOME/.gradle" "$GOINFRE_DIR/.gradle"
  redirect_to_goinfre "$HOME/.pub-cache" "$GOINFRE_DIR/.pub-cache"
fi

# ---- k6 (load testing) ------------------------------------------------------
header "k6 (optional — for load tests)"

K6_BIN="$HOME/.local/bin/k6"

if in_path k6; then
  ok "k6 already installed ($(k6 version 2>&1 | head -1))"
elif [ -x "$K6_BIN" ]; then
  ok "k6 already installed at $K6_BIN"
  ensure_path "$HOME/.local/bin"
  add_profile_line "local/bin" $'# Local binaries\nexport PATH="$PATH:$HOME/.local/bin"'
else
  info "Installing k6 to $K6_BIN"
  ensure_dir "$HOME/.local/bin"
  K6_ARCH="amd64"
  [ "$(uname -m)" = "aarch64" ] && K6_ARCH="arm64"
  wget -q --show-progress \
    "https://github.com/grafana/k6/releases/download/v0.57.0/k6-v0.57.0-linux-${K6_ARCH}.tar.gz" \
    -O /tmp/k6.tar.gz
  tar -xzf /tmp/k6.tar.gz -C /tmp/
  cp "/tmp/k6-v0.57.0-linux-${K6_ARCH}/k6" "$K6_BIN"
  chmod +x "$K6_BIN"
  rm -rf /tmp/k6.tar.gz "/tmp/k6-v0.57.0-linux-${K6_ARCH}"
  ok "k6 v0.57.0 installed"
  ensure_path "$HOME/.local/bin"
  add_profile_line "local/bin" $'# Local binaries\nexport PATH="$PATH:$HOME/.local/bin"'
fi

# ---- Project dependencies ---------------------------------------------------
header "Project dependencies"

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Flutter/Gradle write their build output straight into the project tree
# (build/, android/.gradle) rather than anywhere covered by the SDK/cache
# redirection above. On a small $HOME that output alone (Gradle's merged
# native libs, per-flavor APKs, annotation processing, etc.) is enough to
# fill the partition mid-build ("No space left on device"). Redirect it to
# goinfre the same way as everything else, when goinfre is available.
if [ -n "$GOINFRE_DIR" ]; then
  redirect_to_goinfre "$PROJECT_DIR/build" "$GOINFRE_DIR/project-build/build"
  redirect_to_goinfre "$PROJECT_DIR/android/.gradle" "$GOINFRE_DIR/project-build/android-.gradle"
fi

# Flutter pub get
if [ -x "$FLUTTER_BIN" ]; then
  if [ -f "$PROJECT_DIR/pubspec.yaml" ]; then
    info "Running flutter pub get…"
    (cd "$PROJECT_DIR" && "$FLUTTER_BIN" pub get)
    ok "Flutter dependencies resolved"
  fi
else
  skip "Flutter not found — skipping flutter pub get"
fi

# ---- Summary ----------------------------------------------------------------
header "Installation summary"

report_ok()   { echo -e "  ${GREEN}✓${NC} $*"; }
report_miss() { echo -e "  ${RED}✗${NC} $*"; }

in_path docker        && report_ok "Docker + Compose"       || report_miss "Docker + Compose"
[ -x "$FLUTTER_BIN" ]  && report_ok "Flutter ≥ $FLUTTER_MIN" || report_miss "Flutter ≥ $FLUTTER_MIN"
in_path java           && report_ok "Java ≥ 17"              || report_miss "Java ≥ 17"
[ -x "$SDKMANAGER" ]   && report_ok "Android SDK"            || report_miss "Android SDK"
in_path k6             && report_ok "k6"                     || report_miss "k6"

echo ""
echo "  ${BOLD}Hint:${NC} Run ${BOLD}source $RCFILE${NC} or open a new terminal for new PATH entries."
echo "  ${BOLD}Next:${NC}  make up"
echo "  ${BOLD}Next:${NC}  make web   (browser) | make mobile (Android device)"
