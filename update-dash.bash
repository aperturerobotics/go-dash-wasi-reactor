#!/bin/bash
set -euo pipefail

# Dash WASI Reactor Update Script
# Builds the WASM binary from aperturerobotics/dash master and updates version metadata.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="aperturerobotics/dash"
DASH_DIR="${1:-}"

if [ -z "$DASH_DIR" ]; then
    echo "Usage: $0 /path/to/dash [wasi-sdk-path]"
    echo ""
    echo "Builds dash.wasm from the given dash checkout and copies it here."
    echo "The dash checkout must have been built natively first (./autogen.sh && ./configure && make)"
    echo "to generate parser tables."
    echo ""
    echo "If wasi-sdk-path is not provided, defaults to ~/repos/wasi-sdk."
    exit 1
fi

WASI_SDK="${2:-$HOME/repos/wasi-sdk}"

if [ ! -f "$WASI_SDK/bin/clang" ]; then
    echo "Error: wasi-sdk not found at $WASI_SDK"
    echo "Set the second argument to the wasi-sdk path."
    exit 1
fi

if [ ! -f "$DASH_DIR/src/reactor.c" ]; then
    echo "Error: $DASH_DIR does not look like an aperturerobotics/dash checkout"
    exit 1
fi

DASH_DIR="$(cd "$DASH_DIR" && pwd)"

echo "Dash source: $DASH_DIR"
echo "wasi-sdk: $WASI_SDK"

# Get the commit hash for version tracking.
COMMIT="$(cd "$DASH_DIR" && git rev-parse HEAD)"
SHORT="$(cd "$DASH_DIR" && git rev-parse --short HEAD)"
UPSTREAM_VERSION="$(grep 'PACKAGE_VERSION' "$DASH_DIR/config-wasi.h" | head -1 | sed 's/.*"\(.*\)".*/\1/')"

echo "Dash commit: $SHORT ($UPSTREAM_VERSION)"

# Build WASM reactor binary.
echo "Building WASI reactor..."
BUILD_DIR="$DASH_DIR/build-wasi"
mkdir -p "$BUILD_DIR"
cd "$BUILD_DIR"

cmake "$DASH_DIR" \
    -DCMAKE_SYSTEM_NAME=WASI \
    -DCMAKE_C_COMPILER="$WASI_SDK/bin/clang" \
    -DCMAKE_SYSROOT="$WASI_SDK/share/wasi-sysroot" \
    -DDASH_WASI_REACTOR=ON \
    -DCMAKE_BUILD_TYPE=Release \
    > /dev/null 2>&1

cmake --build . 2>&1 | tail -3

if [ ! -f "$BUILD_DIR/dash.wasm" ]; then
    echo "Error: build failed, dash.wasm not found"
    exit 1
fi

# Copy WASM binary.
cp "$BUILD_DIR/dash.wasm" "$SCRIPT_DIR/dash.wasm"
echo "Copied dash.wasm ($(wc -c < "$SCRIPT_DIR/dash.wasm" | tr -d ' ') bytes)"

# Generate version info Go file.
echo "Generating version.go..."
cat > "$SCRIPT_DIR/version.go" << EOF
package dashwasi

// Dash WASI reactor version information.
const (
	// Version is the upstream dash version this was built from.
	Version = "$UPSTREAM_VERSION"

	// Commit is the aperturerobotics/dash commit hash.
	Commit = "$COMMIT"

	// SourceURL is the repository URL for the dash fork.
	SourceURL = "https://github.com/$REPO"
)
EOF

echo "Generated version.go (dash $UPSTREAM_VERSION, commit $SHORT)"
echo ""
echo "Update complete!"
