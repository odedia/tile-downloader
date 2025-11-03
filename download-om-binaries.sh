#!/bin/bash

# Script to download OM CLI binaries for embedding

set -e

VERSION="7.18.2"
EMBED_DIR="embed/bin"

echo "Downloading OM CLI version ${VERSION}..."
mkdir -p "${EMBED_DIR}"

# macOS Intel
echo "Downloading macOS Intel binary..."
curl -L -o "${EMBED_DIR}/om-darwin-amd64" \
  "https://github.com/pivotal-cf/om/releases/download/${VERSION}/om-darwin-amd64-${VERSION}"

# macOS Apple Silicon
echo "Downloading macOS Apple Silicon binary..."
curl -L -o "${EMBED_DIR}/om-darwin-arm64" \
  "https://github.com/pivotal-cf/om/releases/download/${VERSION}/om-darwin-arm64-${VERSION}"

# Linux AMD64
echo "Downloading Linux AMD64 binary..."
curl -L -o "${EMBED_DIR}/om-linux-amd64" \
  "https://github.com/pivotal-cf/om/releases/download/${VERSION}/om-linux-amd64-${VERSION}"

# Windows AMD64
echo "Downloading Windows AMD64 binary..."
curl -L -o "${EMBED_DIR}/om-windows-amd64.exe" \
  "https://github.com/pivotal-cf/om/releases/download/${VERSION}/om-windows-amd64-${VERSION}.exe"

# Make binaries executable
chmod +x "${EMBED_DIR}"/om-*

echo "✓ All OM CLI binaries downloaded successfully to ${EMBED_DIR}/"
echo "✓ Total size: $(du -sh ${EMBED_DIR} | cut -f1)"
