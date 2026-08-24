# BACKFLASH-releaser

Releaser skapas när en Git-tag med formatet `v*` pushas. GitHub Actions kör
tester och vet, bygger portabla binärer med `CGO_ENABLED=0`, skapar
`SHA256SUMS` och publicerar dem under GitHub Releases.

Lokalt kan samma paket byggas med:

```bash
./deploy/build-release.sh v0.1.0-test
```

Assets blir klientbinärer för Linux, Windows och macOS samt cache-peer för
Linux amd64/arm64.

## Uppdatera cache-servern

När repot är publikt och en release finns kan servern uppdateras med GitHub
CLI. Detta laddar bara ner vald asset; konfiguration och `/var/lib/backflash`
ersätts inte.

```bash
gh release download v0.1.0 --repo Is0cre/flashback-analyzer --pattern 'backflash-cache-linux-amd64' --dir /tmp/backflash-update
```

Verifiera checksumma och installera sedan:

```bash
cd /tmp/backflash-update && sha256sum -c <(curl -fsSL https://github.com/Is0cre/flashback-analyzer/releases/download/v0.1.0/SHA256SUMS | grep 'backflash-cache-linux-amd64')
```

```bash
sudo systemctl stop backflash-cache && sudo install -m 0755 backflash-cache-linux-amd64 /usr/local/bin/backflash-cache && sudo systemctl start backflash-cache && sudo systemctl status backflash-cache --no-pager
```

Privata meshidentiteter, konfiguration och cacheobjekt ligger kvar. Kontrollera
alltid releaseversion och checksumma innan installation.
