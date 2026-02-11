#!/usr/bin/env bash
set -euo pipefail

echo "==> Creating BOSH dev release (offline)..."
bosh create-release --force --tarball=openclaw-release.tgz

echo "==> Building dev tile (offline)..."
tile build dev

echo "==> Dev tile built."
ls -la product/*.pivotal 2>/dev/null || echo "Check output for .pivotal file location."
