#!/usr/bin/env bash
#
# Cross-compile save-token and stage the binaries for the VS Code
# extension bundle + GitHub Release assets.
#
#   ./build.sh              # release binaries for all platforms
#   ./build.sh --local      # current platform only, into ./bin
#
# Outputs:
#   dist/save-token-<version>-<os>-<arch>.tar.gz   (release assets, .zip on Windows)
#   vscode-extension/binaries/save-token-<os>-<arch>[.exe]

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

VERSION="$(sed -n 's/.*version "\([0-9][^"]*\)".*/\1/p' cmd/save-token/main.go | head -1)"
VERSION="${VERSION:-0.0.0-dev}"
DIST="dist"
EXT_BIN="vscode-extension/binaries"

build_one() {
  local os="$1" arch="$2" out="$3"
  echo "  building $os/$arch -> $out"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w" -o "$out" ./cmd/save-token
}

if [ "${1:-}" = "--local" ]; then
  mkdir -p bin
  build_one "$(go env GOOS)" "$(go env GOARCH)" bin/save-token
  echo "ok: bin/save-token"
  exit 0
fi

rm -rf "$DIST" "$EXT_BIN"
mkdir -p "$DIST" "$EXT_BIN"

targets=(
  "darwin arm64"
  "darwin amd64"
  "linux arm64"
  "linux amd64"
  "windows amd64"
  "windows arm64"
)

for t in "${targets[@]}"; do
  os="${t%% *}"; arch="${t##* }"
  bin="save-token"
  [ "$os" = "windows" ] && bin="save-token.exe"
  stage="$(mktemp -d)"
  build_one "$os" "$arch" "$stage/$bin"
  # Extension bundles keep a simple platform-suffixed name.
  cp "$stage/$bin" "$EXT_BIN/save-token-$os-$arch$( [ "$os" = "windows" ] && echo .exe )"
  if [ "$os" = "windows" ]; then
    # Windows users get a zip; Explorer opens it natively.
    (cd "$stage" && zip -qr "$ROOT/$DIST/save-token-$VERSION-$os-$arch.zip" "$bin")
  else
    tar -czf "$DIST/save-token-$VERSION-$os-$arch.tar.gz" -C "$stage" "$bin"
  fi
  rm -rf "$stage"
done

# Checksums for the GitHub Release.
(cd "$DIST" && shasum -a 256 ./*.tar.gz ./*.zip > checksums.txt)

echo
echo "Release assets in $DIST/:"
ls -1 "$DIST"
echo "Extension binaries in $EXT_BIN/:"
ls -1 "$EXT_BIN"
echo
echo "Version: $VERSION"
