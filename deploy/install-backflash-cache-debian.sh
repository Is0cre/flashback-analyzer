#!/usr/bin/env bash
set -euo pipefail

# Installerar en redan byggd backflash-cache-binär på Debian/Ubuntu.
# Kör på servern som root:
#   sudo ./install-backflash-cache-debian.sh ./backflash-cache

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Kör som root, till exempel: sudo $0 ./backflash-cache" >&2
  exit 1
fi

if [[ $# -ne 1 || ! -f "$1" ]]; then
  echo "Användning: $0 /sökväg/till/backflash-cache" >&2
  exit 1
fi

binary="$1"
install -d -m 0755 /etc/backflash /var/lib/backflash

if ! getent group backflash >/dev/null; then
  groupadd --system backflash
fi
if ! id backflash >/dev/null 2>&1; then
  useradd --system --gid backflash --home-dir /var/lib/backflash \
    --no-create-home --shell /usr/sbin/nologin backflash
fi

install -o root -g root -m 0755 "$binary" /usr/local/bin/backflash-cache
install -o root -g root -m 0644 "$(dirname "$0")/backflash-cache.service" \
  /etc/systemd/system/backflash-cache.service

if [[ ! -e /etc/backflash/config.toml ]]; then
  install -o root -g backflash -m 0640 \
    "$(dirname "$0")/backflash-cache.toml.example" /etc/backflash/config.toml
  echo "Skapade /etc/backflash/config.toml — redigera peers och starta sedan tjänsten."
else
  echo "Behåller befintlig /etc/backflash/config.toml"
fi

chown -R backflash:backflash /var/lib/backflash
systemctl daemon-reload
systemctl enable backflash-cache.service
systemctl restart backflash-cache.service

echo
echo "BACKFLASH cache-peer installerad."
echo "Status: systemctl status backflash-cache"
echo "Logg:   journalctl -u backflash-cache -f"
echo "Konfig: /etc/backflash/config.toml"
