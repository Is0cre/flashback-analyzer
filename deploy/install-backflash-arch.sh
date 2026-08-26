#!/usr/bin/env bash
set -euo pipefail

# Installerar BACKFLASH-klienten på Arch Linux från en publik GitHub Release.
# Den ändrar inte användarens lokala databas eller konfiguration.

repo="${BACKFLASH_REPO:-Is0cre/flashback-analyzer}"
version="${1:-v0.1.0-beta.2}"
prefix="${BACKFLASH_PREFIX:-/usr/local/bin}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Kör som root: sudo $0 $version" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) asset="backflash-linux-amd64" ;;
  aarch64|arm64) asset="backflash-linux-arm64" ;;
  *) echo "CPU stöds inte: $(uname -m)" >&2; exit 1 ;;
esac

base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d /tmp/backflash-arch.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT

curl --fail --location --silent --show-error --retry 3 -o "$tmp/$asset" "$base/$asset"
curl --fail --location --silent --show-error --retry 3 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
grep -E "[[:space:]]${asset}$" "$tmp/SHA256SUMS" > "$tmp/SHA256SUMS.selected"
(cd "$tmp" && sha256sum -c SHA256SUMS.selected)

install -d -m 0755 "$prefix"
if [[ -x "$prefix/backflash" ]]; then
  cp -a "$prefix/backflash" "$prefix/backflash.previous"
fi
install -m 0755 "$tmp/$asset" "$prefix/backflash"

echo "BACKFLASH ${version} installerad i ${prefix}/backflash (${asset})."
echo "Starta med: backflash"
