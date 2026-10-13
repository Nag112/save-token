#!/usr/bin/env bash
#
# save-token installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Nag112/save-token/main/install.sh | bash
#   ./install.sh                  # install from a local checkout (builds with Go)
#   ./install.sh --from-release   # force download from GitHub Releases
#
# Installs the save-token binary to ~/.local/bin (override with
# SAVE_TOKEN_INSTALL_DIR). Adds a PATH hint to your shell rc if needed.

set -euo pipefail

REPO="Nag112/save-token"
VERSION="${SAVE_TOKEN_VERSION:-0.2.0}"
INSTALL_DIR="${SAVE_TOKEN_INSTALL_DIR:-$HOME/.local/bin}"
TARGET="$INSTALL_DIR/save-token"
FROM_RELEASE=0
LOCAL_MODE=0

for arg in "$@"; do
  case "$arg" in
    --from-release) FROM_RELEASE=1 ;;
    --help|-h)
      sed -n '2,12p' "$0"
      exit 0
      ;;
    *)
      echo "[save-token] unknown option: $arg" >&2
      exit 2
      ;;
  esac
done

info() { printf '[save-token] %s\n' "$*"; }
die()  { printf '[save-token] error: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Platform detection (release asset naming: save-token-<os>-<arch>.tar.gz)
# ---------------------------------------------------------------------------
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)      die "unsupported OS: $(uname -s) (on Windows use install.ps1 in PowerShell, WSL, or the VS Code extension)" ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64)  arch="amd64" ;;
    *)             die "unsupported architecture: $(uname -m)" ;;
  esac
  PLATFORM="$os"
  ARCH="$arch"
}

# ---------------------------------------------------------------------------
# Install sources
# ---------------------------------------------------------------------------
install_from_release() {
  detect_platform
  local asset="save-token-${VERSION}-${PLATFORM}-${ARCH}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${VERSION}/${asset}"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  info "Downloading ${url}"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmp/$asset" || die "download failed: $url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$tmp/$asset" || die "download failed: $url"
  else
    die "need curl or wget to download the release"
  fi

  tar -xzf "$tmp/$asset" -C "$tmp"
  [ -f "$tmp/save-token" ] || die "archive did not contain a save-token binary"
  mv "$tmp/save-token" "$TARGET"
}

install_from_local() {
  local src
  src="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local bin="$src/bin/save-token"

  # Prefer a prebuilt binary; otherwise build from source.
  if [ ! -x "$bin" ]; then
    command -v go >/dev/null 2>&1 || die "no prebuilt binary at $bin and go is not installed (install Go or run ./build.sh first)"
    info "Building from source at $src"
    (cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/save-token ./cmd/save-token)
  fi
  cp "$bin" "$TARGET"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
mkdir -p "$INSTALL_DIR"

# Local checkout wins unless --from-release was passed explicitly.
script_self="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"
if [ "$FROM_RELEASE" -eq 0 ] && [ -n "$script_self" ] && [ -f "$script_self/go.mod" ]; then
  LOCAL_MODE=1
fi

if [ "$LOCAL_MODE" -eq 1 ]; then
  install_from_local
else
  install_from_release
fi

chmod +x "$TARGET"

# macOS: strip quarantine attr so Gatekeeper doesn't block the download.
if [ "$(uname -s)" = "Darwin" ]; then
  xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
fi

info "Installed $("$TARGET" --version 2>/dev/null || echo "save-token") to $TARGET"

# PATH check
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    info "$INSTALL_DIR is not on your PATH."
    shell_rc="$HOME/.zshrc"
    [ -n "${BASH_VERSION:-}" ] && shell_rc="$HOME/.bashrc"
    info "Add this to $shell_rc and restart your shell:"
    echo
    echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
    echo
    ;;
esac

cat <<'EOF'
[save-token] Done. Try it:
    save-token git status
    save-token --dry-run pytest

[save-token] Agents: point your coding agent at an instruction file telling
[save-token] it to run terminal commands as `save-token <command>`.
EOF
