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

När repot är publikt behövs ingen GitHub-inloggning på cache-servern. Första
installationen laddar ner rätt binär för serverns arkitektur, verifierar
`SHA256SUMS` och skapar systemd-tjänsten. Konfiguration, mesh-identitet och
`/var/lib/backflash` ersätts inte om de redan finns.

```bash
curl -fsSL https://github.com/Is0cre/flashback-analyzer/releases/download/v0.1.0-beta.2/install-backflash-cache-release-debian.sh \
  -o /tmp/install-backflash-cache-release-debian.sh \
  && sudo bash /tmp/install-backflash-cache-release-debian.sh v0.1.0-beta.2
```

Efter det uppdateras servern med en enda hämtning av uppdateringsskriptet:

```bash
curl -fsSL https://github.com/Is0cre/flashback-analyzer/releases/download/v0.1.0-beta.2/update-backflash-cache-debian.sh \
  -o /tmp/update-backflash-cache-debian.sh \
  && sudo bash /tmp/update-backflash-cache-debian.sh v0.1.0-beta.2
```

Uppdateraren stoppar tjänsten, verifierar checksumma, sparar föregående binär
och återställer den automatiskt om den nya versionen inte kan starta. Privata
meshidentiteter, konfiguration och cacheobjekt ligger kvar.
