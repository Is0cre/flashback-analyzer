#!/usr/bin/env bash
set -euo pipefail

# Installs the BACKFLASH E2E-CHATT seed daemon (gandrd) from a public GitHub
# Release on Debian/Ubuntu — no Go toolchain or build step on the server.
# Config, identity and passphrase are created/kept locally; re-running this
# script only upgrades the binary.
#
# Usage: sudo ./install-gandrd-release-debian.sh [version]   (default: latest tag below)

repo="${BACKFLASH_REPO:-Is0cre/flashback-analyzer}"
version="${1:-v0.1.0-beta.4}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Kör som root: sudo $0 $version" >&2
  exit 1
fi
command -v systemctl >/dev/null || { echo "systemd krävs" >&2; exit 1; }

case "$(uname -m)" in
  x86_64) asset="gandrd-linux-amd64" ;;
  aarch64|arm64) asset="gandrd-linux-arm64" ;;
  *) echo "CPU stöds inte: $(uname -m)" >&2; exit 1 ;;
esac

base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d /tmp/gandrd-install.XXXXXX)"
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
if [[ -f "$script_dir/gandrd.service" && -f "$script_dir/gandrd.toml.example" ]]; then
  service_file="$script_dir/gandrd.service"
  config_example="$script_dir/gandrd.toml.example"
else
  download -o "$tmp/gandrd.service" "$base/gandrd.service"
  download -o "$tmp/gandrd.toml.example" "$base/gandrd.toml.example"
  service_file="$tmp/gandrd.service"
  config_example="$tmp/gandrd.toml.example"
fi

CONFIG_DIR=/etc/gandrd
STATE_DIR=/var/lib/gandrd

if ! getent group gandrd >/dev/null; then
  groupadd --system gandrd
fi
if ! id gandrd >/dev/null 2>&1; then
  useradd --system --gid gandrd --home-dir "$STATE_DIR" \
    --no-create-home --shell /usr/sbin/nologin gandrd
fi
install -d -m 0750 -o root -g gandrd "$CONFIG_DIR"
install -d -m 0700 -o gandrd -g gandrd "$STATE_DIR"

if [[ -x /usr/local/bin/gandrd ]]; then
  cp -a /usr/local/bin/gandrd /usr/local/bin/gandrd.previous
fi
install -o root -g root -m 0755 "$tmp/$asset" /usr/local/bin/gandrd
install -o root -g root -m 0644 "$service_file" /etc/systemd/system/gandrd.service

# Unattended-start passphrase: protects the keyfiles at rest, not against
# live root compromise. Generated once, never overwritten.
PASSFILE="$CONFIG_DIR/passphrase"
if [[ ! -f "$PASSFILE" ]]; then
  umask 077
  head -c 32 /dev/urandom | base64 | tr -d '\n=' > "$PASSFILE"
  chown root:gandrd "$PASSFILE"
  chmod 0640 "$PASSFILE"
  echo "Skapade $PASSFILE"
fi

# ProtectSystem=strict makes /etc read-only for the running service, so the
# identity keyfile has to live under $STATE_DIR, not /etc — rewrite the
# example's placeholder paths the same way the source install.sh does.
CONFIG="$CONFIG_DIR/config.toml"
if [[ ! -e "$CONFIG" ]]; then
  sed -e "s|^keyfile = .*|keyfile = \"$STATE_DIR/identity.key\"|" \
      -e "s|^# passphrase_file = .*|passphrase_file = \"$PASSFILE\"|" \
      "$config_example" > "$CONFIG"
  chown root:gandrd "$CONFIG"
  chmod 0640 "$CONFIG"
  echo "Skapade $CONFIG — redigera [network] peers, lägg till egna yggdrasil-peers, och starta sedan tjänsten."
else
  echo "Behåller befintlig $CONFIG"
fi

systemctl daemon-reload
systemctl enable gandrd.service
if ! systemctl restart gandrd.service; then
  echo "Tjänsten kunde inte starta. Den nya binären ligger kvar för felsökning." >&2
  exit 1
fi

echo
echo "gandrd installerad från ${version}."
echo "Status: systemctl status gandrd"
echo "Logg:   journalctl -u gandrd -f"
echo "Konfig: $CONFIG"
echo
echo "Nodens yggdrasil-nyckel (klistra in i internal/tui/app.go som"
echo "defaultSeedYggdrasilKey, eller BACKFLASH_SEED_KEY på klienten):"
journalctl -u gandrd -n 20 --no-pager | grep "yggdrasil node key" | tail -1 || \
  echo "  (inte i loggen än — kör: journalctl -u gandrd -f)"
