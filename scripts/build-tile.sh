#!/usr/bin/env bash
set -euo pipefail

echo "==> Creating BOSH release..."
bosh create-release --force --tarball=openclaw-release.tgz

echo "==> Building Ops Manager tile..."
tile build

echo "==> Tile built successfully."
ls -la product/*.pivotal 2>/dev/null || echo "Check output for .pivotal file location."
