#!/usr/bin/env bash
# Build script for Brockchain Go client (Linux/macOS/Windows)

set -e

echo "🔨 Building Brockchain Go client..."

cd "$(dirname "$0")"

# Output directory
OUTPUT_DIR="dist"
mkdir -p "$OUTPUT_DIR"

# Build targets
declare -a TARGETS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
)

for target in "${TARGETS[@]}"; do
    IFS='/' read -r GOOS GOARCH <<< "$target"
    
    OUTPUT_FILE="brockchain-client"
    if [ "$GOOS" = "windows" ]; then
        OUTPUT_FILE="brockchain-client.exe"
    fi
    
    echo "📦 Building $GOOS/$GOARCH -> $OUTPUT_DIR/$OUTPUT_FILE"
    
    GOOS="$GOOS" GOARCH="$GOARCH" go build -o "$OUTPUT_DIR/$OUTPUT_FILE" .
    
    # Rename for clarity
    if [ "$GOOS" != "windows" ]; then
        mv "$OUTPUT_DIR/$OUTPUT_FILE" "$OUTPUT_DIR/brockchain-client-$GOOS-$GOARCH"
    else
        mv "$OUTPUT_DIR/$OUTPUT_FILE" "$OUTPUT_DIR/brockchain-client-$GOOS-$GOARCH.exe"
    fi
done

echo "✓ Build complete!"
echo "Binaries:"
ls -lh "$OUTPUT_DIR"
