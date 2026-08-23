# BACKFLASH cache-peer – Debian/Ubuntu

Detta paket innehåller den headlessa BACKFLASH-cache-peeren. Den kör ingen
TUI, öppnar ingen Flashback-session och använder inte GANDR-identitet eller
GANDR-lagring.

## Installation

Paketet är byggt för Linux amd64.

```bash
unzip backflash-cache-server-debian-amd64.zip
cd backflash-cache-server-debian-amd64
sudo ./install-backflash-cache-debian.sh ./backflash-cache
```

Redigera därefter:

```bash
sudoedit /etc/backflash/config.toml
```

Ange serverns peer och behåll porten som redan är forwardad i brandväggen:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = []
```

Starta om tjänsten:

```bash
sudo systemctl restart backflash-cache
sudo systemctl status backflash-cache
sudo journalctl -u backflash-cache -f
```

## Brandvägg

Tillåt endast den port/protokollkombination som används för Yggdrasil-peering.
I den aktuella setupen är det:

```text
TCP 4242 från utvalda peers eller enligt din peer-policy
```

Ingen webbserver, adminpanel eller SSH-exponering installeras av paketet.

## Lagring och identitet

Tjänsten kör som systemanvändaren `backflash` och använder:

```text
/var/lib/backflash
```

Mesh-identiteten skapas lokalt när tjänsten startar första gången och får
aldrig delas med GANDR eller Flashback. Säkerhetskopiera identiteten om samma
peer ska kunna återställas efter en VM-flytt.

## Kontroll

```bash
sudo systemctl is-active backflash-cache
sudo journalctl -u backflash-cache --no-pager -n 50
```

Om tjänsten inte startar, kontrollera först konfigurationen och att porten
inte redan används. BACKFLASH fortsätter fungera som lokal Flashback-klient
även om cache-peeren är nere.
