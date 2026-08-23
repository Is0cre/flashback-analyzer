# BACKFLASH cache-peer på Debian/Ubuntu

Cache-peer-läget är en separat, headless process. Den kör inte TUI, öppnar
ingen Flashback-session och använder inte GANDR-identitet eller GANDR-lagring.

## Bygg binären

Bygg på en utvecklingsmaskin för serverns arkitektur:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -o backflash-cache ./cmd/backflash-cache
```

För ARM64 används `GOARCH=arm64`.

Kopiera `backflash-cache` samt katalogen `deploy/` till servern och kör:

```bash
sudo ./deploy/install-backflash-cache-debian.sh ./backflash-cache
```

Installationsskriptet:

- skapar den separata systemanvändaren `backflash`
- installerar binär och systemd-enhet
- skapar inte om en befintlig konfiguration
- sparar mesh-data under `/var/lib/backflash`
- startar om endast `backflash-cache.service`

## Konfiguration

Redigera:

```text
/etc/backflash/config.toml
```

Exempel:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = ["tcp://[YGGDRASIL-PEER]:4242"]
```

Öppna endast den port och det protokoll som din Yggdrasil-konfiguration
faktiskt använder. `0.0.0.0` i exemplet är inte en ersättning för korrekt
Yggdrasil-peering eller brandväggsregler.

## Drift

```bash
systemctl status backflash-cache
journalctl -u backflash-cache -f
systemctl restart backflash-cache
```

Visa serverns publika mesh-nyckel för klientens `peer_key` utan att skriva ut
den privata seed-filen:

```bash
sudo -u backflash BACKFLASH_CONFIG=/etc/backflash/config.toml \
  XDG_DATA_HOME=/var/lib/backflash \
  /usr/local/bin/backflash-cache identity
```

Kommandot skriver en 64 tecken lång hexsträng. Det är den publika nyckeln som
ska användas på klienten. Klistra aldrig in `identity.key` i konfigurationen.

Tjänsten använder `Restart=on-failure`. Vid nätverksproblem fortsätter lokal
cache att finnas kvar och processen försöker starta om utan att någon central
cache eller registrering krävs.

Mesh-identiteten skapas först när tjänsten faktiskt startar och sparas lokalt
i `/var/lib/backflash/backflash/mesh/identity.key`. Säkerhetskopiera den bara
om samma peer-identitet ska behållas; kopiera den inte till GANDR eller andra
BACKFLASH-installationer.
