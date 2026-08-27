# BACKFLASH E2E-CHATT-seed (gandrd) på Debian/Ubuntu

`gandrd` är den fristående Gandr-daemonen (se [src/gandr/README.md](../src/gandr/README.md)):
en inbäddad Yggdrasil-nod plus federationsmotor, helt separat från
BACKFLASH:s Flashback-läsning och cache-mesh. Att köra den som seed innebär
bara att andra klienter kan hitta den som en välkänd första kontakt — den
ser aldrig meddelandeinnehåll och lagrar inget identitetskopplat.

## Installation

Ingen Go-toolchain eller byggsteg krävs på servern. Binären hämtas från en
publik GitHub Release, verifieras mot `SHA256SUMS`, och installeras som en
systemd-tjänst:

```bash
sudo ./deploy/install-gandrd-release-debian.sh v0.1.0-beta.4
```

Skriptet:

- skapar systemanvändaren `gandrd`
- installerar binär och systemd-enhet
- genererar en lösenordsfras för obevakad start (`/etc/gandrd/passphrase`)
- skriver `/etc/gandrd/config.toml` från mallen, med `seed_node = true` och
  `capabilities.seed = true` redan påslaget — skapar den inte om den redan
  finns
- startar och aktiverar `gandrd.service`

Uppdatera senare genom att köra skriptet igen med en ny version; befintlig
konfiguration och identitet rörs inte.

## Konfiguration

Redigera `/etc/gandrd/config.toml` och lägg till minst en
`[network] peers`-post (publika Yggdrasil-peers, eller en direkt
`tcp://host:port`-överenskommelse) — utan det når noden aldrig overlayen.
Se `[network] listen` för vilken port andra noder kan nå den på.

## Hitta nodens Yggdrasil-nyckel

Detta är nyckeln andra klienter (eller BACKFLASH:s inbyggda
`defaultSeedYggdrasilKey`) pekar på för att federera med den här noden som
seed. Den skrivs till stderr vid varje daemonstart, inte till en fil:

```bash
sudo journalctl -u gandrd -n 50 --no-pager | grep "yggdrasil node key"
```

eller live vid omstart:

```bash
sudo systemctl restart gandrd
sudo journalctl -u gandrd -f
```

## Drift

```bash
systemctl status gandrd
journalctl -u gandrd -f
systemctl restart gandrd
```

Tjänsten kör med `ProtectSystem=strict`; identitetsnyckeln lever därför
under `/var/lib/gandrd`, inte `/etc`. Säkerhetskopiera
`/var/lib/gandrd/identity.key` tillsammans med `/etc/gandrd/passphrase` —
utan båda är nodens identitet oåterkallelig.
