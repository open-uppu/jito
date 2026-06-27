#!/usr/bin/env bash
# jito installer — `curl -fsSL ... | bash`
set -euo pipefail

REPO="uppu/jito"
BINARY="jito"
INSTALL_DIR="${JITO_INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS / Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported arch: $ARCH"; exit 1 ;;
esac

echo "⚡ Installing jito ($OS/$ARCH) to $INSTALL_DIR"

mkdir -p "$INSTALL_DIR"

# For now, build from source (no prebuilt binaries yet)
if command -v go >/dev/null 2>&1; then
  TMP=$(mktemp -d)
  git clone "https://github.com/$REPO.git" "$TMP/jito" 2>/dev/null || {
    echo "⚠️  Git clone failed, falling back to go install"
    GOBIN="$INSTALL_DIR" go install "github.com/$REPO/cmd/$BINARY@latest"
    echo "✅ jito installed via go install"
    echo "👉 Add to PATH: export PATH=\$PATH:$INSTALL_DIR"
    exit 0
  }
  (cd "$TMP/jito" && GOBIN="$INSTALL_DIR" go install ./cmd/jito)
  rm -rf "$TMP"
  echo "✅ jito installed (built from source)"
else
  echo "❌ Go not found. Install Go 1.22+ first: https://go.dev/dl/"
  exit 1
fi

echo "👉 Add to PATH if not already: export PATH=\$PATH:$INSTALL_DIR"
echo "👉 Initialize: jito init"
echo "👉 Set API key: export JITO_API_KEY=sk-..."
echo "👉 Test: jito run 'hello world'"