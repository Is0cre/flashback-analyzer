#!/usr/bin/env bash
set -euo pipefail

# Första installation från en publik GitHub Release på Debian/Ubuntu.
# Konfiguration, mesh-identitet och cachedata skapas/behålls lokalt på servern.

repo="${BACKFLASH_REPO:-Is0cre/flashback-analyzer}"
version="${1:-v0.1.0-beta.1}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Kör som root: sudo $0 $version" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) asset="backflash-cache-linux-amd64" ;;
  aarch64|arm64) asset="backflash-cache-linux-arm64" ;;
  *) echo "CPU stöds inte: $(uname -m)" >&2; exit 1 ;;
esac

base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d /tmp/backflash-install.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT

download() {
  curl --fail --location --silent --show-error --retry 3 "$@"
}

download -o "$tmp/$asset" "$base/$asset"
download -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
grep -E "[[:space:]]${asset}$" "$tmp/SHA256SUMS" > "$tmp/SHA256SUMS.selected"
(cd "$tmp" && sha256sum -c SHA256SUMS.selected)
chmod 0755 "$tmp/$asset"

script_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ -f "$script_dir/backflash-cache.service" && -f "$script_dir/backflash-cache.toml.example" ]]; then
  service_file="$script_dir/backflash-cache.service"
  config_file="$script_dir/backflash-cache.toml.example"
else
  download -o "$tmp/backflash-cache.service" "$base/backflash-cache.service"
  download -o "$tmp/backflash-cache.toml.example" "$base/backflash-cache.toml.example"
  service_file="$tmp/backflash-cache.service"
  config_file="$tmp/backflash-cache.toml.example"
fi

install -d -m 0755 /etc/backflash /var/lib/backflash
if ! getent group backflash >/dev/null; then
  groupadd --system backflash
fi
if ! id backflash >/dev/null 2>&1; then
  useradd --system --gid backflash --home-dir /var/lib/backflash \
    --no-create-home --shell /usr/sbin/nologin backflash
fi

if [[ -x /usr/local/bin/backflash-cache ]]; then
  cp -a /usr/local/bin/backflash-cache /usr/local/bin/backflash-cache.previous
fi
install -o root -g root -m 0755 "$tmp/$asset" /usr/local/bin/backflash-cache
install -o root -g root -m 0644 "$service_file" \
  /etc/systemd/system/backflash-cache.service

if [[ ! -e /etc/backflash/config.toml ]]; then
  install -o root -g backflash -m 0640 "$config_file" /etc/backflash/config.toml
  echo "Skapade /etc/backflash/config.toml — redigera peers och starta sedan tjänsten."
else
  echo "Behåller befintlig /etc/backflash/config.toml"
fi

chown -R backflash:backflash /var/lib/backflash
systemctl daemon-reload
systemctl enable backflash-cache.service
if ! systemctl restart backflash-cache.service; then
  echo "Tjänsten kunde inte starta. Den nya binären ligger kvar för felsökning." >&2
  exit 1
fi

echo
echo "BACKFLASH cache-peer installerad från ${version}."
echo "Status: systemctl status backflash-cache"
echo "Logg:   journalctl -u backflash-cache -f"
echo "Konfig: /etc/backflash/config.toml"
