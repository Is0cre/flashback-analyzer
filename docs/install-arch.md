# BACKFLASH på Arch Linux

Installera den färdiga Linux-binären från en publik GitHub Release. Skriptet
väljer amd64 eller arm64, verifierar `SHA256SUMS` och ersätter bara klientens
binär. Lokal databas och konfiguration lämnas orörda.

```bash
curl -fsSL https://github.com/Is0cre/flashback-analyzer/releases/download/v0.1.0-beta.2/install-backflash-arch.sh \
  -o /tmp/install-backflash-arch.sh \
  && sudo bash /tmp/install-backflash-arch.sh v0.1.0-beta.2
```

Starta sedan:

```bash
backflash
```

Skriptet installerar till `/usr/local/bin/backflash`. För AUR kan samma
releasebinär senare paketeras i en `PKGBUILD`; den här varianten kräver varken
Go, Python eller en AUR-helper på användarens maskin.
