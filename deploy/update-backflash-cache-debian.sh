#!/usr/bin/env bash
set -euo pipefail

repo="${BACKFLASH_REPO:-Is0cre/flashback-analyzer}"
version="${1:-v0.1.0-beta.1}"
if [[ "$(id -u)" -ne 0 ]]; then
  echo "Kör som root: sudo $0 $version" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) asset="backflash-cache-linux-amd64" ;;
  aarch64|arm64) asset="backflash-cache-linux-arm64" ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d /tmp/backflash-update.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
curl --fail --location --silent --show-error --retry 3 -o "$tmp/$asset" "$base/$asset"
curl --fail --location --silent --show-error --retry 3 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
grep -E "[[:space:]]${asset}$" "$tmp/SHA256SUMS" > "$tmp/SHA256SUMS.selected"
(cd "$tmp" && sha256sum -c SHA256SUMS.selected)
chmod 0755 "$tmp/$asset"

if [[ -x /usr/local/bin/backflash-cache ]]; then
  cp -a /usr/local/bin/backflash-cache /usr/local/bin/backflash-cache.previous
fi
systemctl stop backflash-cache.service
install -o root -g root -m 0755 "$tmp/$asset" /usr/local/bin/backflash-cache
if ! systemctl start backflash-cache.service; then
  echo "Ny version startade inte; återställer föregående binär." >&2
  if [[ -x /usr/local/bin/backflash-cache.previous ]]; then
    install -o root -g root -m 0755 /usr/local/bin/backflash-cache.previous /usr/local/bin/backflash-cache
    systemctl start backflash-cache.service || true
  fi
  exit 1
fi

echo "BACKFLASH cache uppdaterad till ${version} (${asset})."
systemctl --no-pager --full status backflash-cache.service
