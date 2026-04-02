#!/usr/bin/env bash
# Build script for Brockchain TypeScript client

set -e

echo "🔨 Building Brockchain TypeScript client..."

npm install --legacy-peer-deps
npm run build

echo "✓ Build complete!"
echo "Output: lib/"
ls -lh lib/
