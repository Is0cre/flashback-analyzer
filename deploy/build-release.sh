#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
built="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
out="dist/release/${version}"
rm -rf "$out"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
ldflags="-s -w -X main.version=${version} -X main.commit=${commit} -X main.built=${built}"

build_client() {
  local goos="$1" goarch="$2" suffix=""
  [[ "$goos" == "windows" ]] && suffix=".exe"
  local name="backflash-${goos}-${goarch}${suffix}"
  echo "bygg ${name}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$out/$name" ./cmd/backflash
}

build_cache() {
  local goarch="$1"
  local name="backflash-cache-linux-${goarch}"
  echo "bygg ${name}"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$out/$name" ./cmd/backflash-cache
}

# gandrd is a separate Go module (src/gandr, module github.com/gandr-net/gandr)
# with its own go.mod — build it from within that directory. It's pure Go
# (CGO_ENABLED=0 builds cleanly cross-arch); only the interactive `gandr`
# client needs CGO for sqlite, and a headless seed server never runs that.
build_gandrd() {
  local goarch="$1"
  local name="gandrd-linux-${goarch}"
  echo "bygg ${name}"
  (cd src/gandr && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.Version=${version} -X main.BuildDate=${built}" \
    -o "$out/$name" ./cmd/gandrd)
}

for target in "linux amd64" "linux arm64" "windows amd64" "windows arm64" "darwin amd64" "darwin arm64"; do
  read -r goos goarch <<< "$target"
  build_client "$goos" "$goarch"
done
build_cache amd64
build_cache arm64
build_gandrd amd64
build_gandrd arm64

cp deploy/install-backflash-windows.ps1 deploy/backflash-cache.service deploy/backflash-cache.toml.example \
  deploy/install-backflash-cache-debian.sh deploy/install-backflash-cache-release-debian.sh \
  deploy/update-backflash-cache-debian.sh deploy/install-backflash-arch.sh "$out/"
cp deploy/backflash-network.toml "$out/"
cp deploy/gandrd.service deploy/gandrd.toml.example deploy/install-gandrd-release-debian.sh "$out/"
cp docs/install-windows.md docs/install-arch.md docs/cache-node-debian.md docs/gandrd-seed-debian.md "$out/"
(cd "$out" && sha256sum * > SHA256SUMS)
echo "release assets: $out"
