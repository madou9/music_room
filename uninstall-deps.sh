#!/usr/bin/env bash
#
# uninstall-deps.sh — Remove dependencies installed by install-deps.sh.
#
# Usage:  bash uninstall-deps.sh
#
# Removes everything that install-deps.sh may have set up, while keeping
# system-installed tools (docker, wget, curl, unzip, java) untouched.
#

set -euo pipefail

RED='\033[0;31m';    GREEN='\033[0;32m'
YELLOW='\033[1;33m'; BLUE='\033[0;34m'
BOLD='\033[1m';      NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
header() { echo -e "\n${BOLD}── $* ──${NC}"; }

case "${SHELL##*/}" in
  zsh)  RCFILE="$HOME/.zshrc" ;;
  bash) RCFILE="$HOME/.bashrc" ;;
  *)    RCFILE="$HOME/.bashrc" ;;
esac

echo ""
echo "  This will remove:"
echo "    - ~/flutter          (Flutter SDK)"
echo "    - ~/Android/Sdk      (Android SDK + cmdline-tools)"
echo "    - ~/.local/bin/k6    (k6)"
echo "    - ~/.local/jdk-17    (local JDK 17, if installed)"
echo "    - PATH entries from $RCFILE"
echo ""
read -r -p "Type 'y' to proceed: " ans
[ "$ans" = "y" ] || { echo "Aborted."; exit 1; }

header "Removing installed SDKs and tools"

for dir in "$HOME/flutter" "$HOME/Android/Sdk" "$HOME/.local/jdk-17"; do
  if [ -d "$dir" ]; then
    rm -rf "$dir"
    ok "Removed $dir"
  else
    info "$dir not found — skipping"
  fi
done

if [ -f "$HOME/.local/bin/k6" ]; then
  rm -f "$HOME/.local/bin/k6"
  ok "Removed ~/.local/bin/k6"
  rmdir "$HOME/.local/bin" 2>/dev/null || true
  rmdir "$HOME/.local" 2>/dev/null || true
else
  info "k6 not found — skipping"
fi

header "Cleaning PATH entries from $RCFILE"

if [ -f "$RCFILE" ]; then
  cp "$RCFILE" "${RCFILE}.bak.$(date +%s)"
  info "Backup saved"

  sed -i '/^# Flutter SDK$/d' "$RCFILE"
  sed -i '\|export PATH="$PATH:$HOME/flutter/bin"|d' "$RCFILE"

  sed -i '/^# Android SDK$/d' "$RCFILE"
  sed -i '/^export ANDROID_HOME/d' "$RCFILE"
  sed -i '\|export PATH="$PATH:$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator"|d' "$RCFILE"

  sed -i '/^# Java JDK 17$/d' "$RCFILE"
  sed -i '\|export PATH="$PATH:$HOME/.local/jdk-17/bin"|d' "$RCFILE"

  sed -i '/^# Local binaries$/d' "$RCFILE"
  sed -i '\|export PATH="$PATH:$HOME/.local/bin"|d' "$RCFILE"

  ok "Cleaned PATH entries from $RCFILE"
else
  info "$RCFILE not found — skipping"
fi

header "Done"
echo "  Run ${BOLD}source $RCFILE${NC} or open a new terminal for the cleaned PATH."
