#!/usr/bin/env bash
# Build the CI image, and push it with --push. Runs the same locally and in .github/workflows/ci-image.yml.
set -euo pipefail
cd "$(dirname "$0")/.."
tag="${CI_IMAGE_TAG:-1}"
image="ghcr.io/dovholuknf/atrium-ci:$tag"
go_version="$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)"
pw_range="$(sed -n 's/.*"@playwright\/test": "\([^"]*\)".*/\1/p' package.json)"
pw_version="$(npm view "@playwright/test@$pw_range" version | tail -1 | awk '{print $NF}' | tr -d "'")"
docker build -f .github/ci-image/Dockerfile --build-arg "GO_VERSION=$go_version" --build-arg "PLAYWRIGHT_VERSION=$pw_version" -t "$image" .github/ci-image
echo "built $image (go $go_version, playwright $pw_version)"
if [ "${1:-}" = "--push" ]; then
  docker push "$image"
  echo "pushed $image"
fi
