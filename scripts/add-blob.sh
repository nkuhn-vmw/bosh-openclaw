#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?Usage: $0 <openclaw-version>}"
NODE_VERSION="22.13.1"

echo "==> Downloading OpenClaw v${VERSION}..."
mkdir -p /tmp/openclaw-blobs
cd /tmp/openclaw-blobs

# Download OpenClaw npm tarball
npm pack "openclaw@${VERSION}" 2>/dev/null || echo "WARN: npm pack failed. Place openclaw-${VERSION}.tgz manually."

# Download Node.js prebuilt binary (must match CI and packaging script expectations)
if [ ! -f "node-v${NODE_VERSION}-linux-x64.tar.gz" ]; then
  echo "==> Downloading Node.js v${NODE_VERSION} binary..."
  curl --fail -sSL -o "node-v${NODE_VERSION}-linux-x64.tar.gz" \
    "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.gz"
fi

cd -

echo "==> Adding blobs to BOSH release..."
bosh add-blob "/tmp/openclaw-blobs/openclaw-${VERSION}.tgz" "openclaw/openclaw-${VERSION}.tgz"
bosh add-blob "/tmp/openclaw-blobs/node-v${NODE_VERSION}-linux-x64.tar.gz" "node/node-v${NODE_VERSION}-linux-x64.tar.gz"

echo "==> Done. Run 'bosh create-release --force' to build."
