# BACKFLASH: fysisk tvåmaskinstest av mesh

Det här testet använder BACKFLASH:s inbyggda Yggdrasil-transport. Ingen
extern Yggdrasil-daemon och ingen GANDR-komponent behövs.

Testet använder två separata datakataloger och två separata BACKFLASH-
meshidentiteter. Kör kommandona från repositoryts rot på respektive dator.

## Vad adresserna betyder

`[mesh].peers` är ett vanligt Yggdrasil-underlay-endpoint, till exempel:

```text
tcp://192.0.2.10:4242
```

Det måste vara nåbart mellan datorerna och tillåtet i brandvägg/NAT.

`[mesh].peer_key` är den andra BACKFLASH-nodens 64 tecken långa Yggdrasil-
publika nyckel. Den är inte den privata `identity.key`-filen.

`backflash-cache identity` skriver dessutom ut nodens härledda Yggdrasil-
overlayadress. BACKFLASH-protokollet adresserar dock peers med publika nycklar;
overlayadressen är främst användbar för diagnostik.

## Fas 1: bygg och verifiera miljön

På båda datorerna:

```bash
go version
python --version
git rev-parse --short HEAD
go test ./...
go vet ./...
go build ./...
```

Projektet deklarerar:

```text
go 1.26.4
```

Go 1.26.4 är miniminivån som repositoryt deklarerar. Go 1.27.x fungerar också.
Python används endast av den äldre referensimplementationen och dess tester;
Python 3.14.6 och 3.14.7 ska inte påverka Go-klienten.

Bygg båda binärerna:

```bash
go build -o backflash ./cmd/backflash
go build -o backflash-cache ./cmd/backflash-cache
```

## Fas 2: skapa identitet utan att dela nyckelfiler

Använd olika datahem på A och B. Det gör testet lätt att återställa och
förhindrar att samma meshidentitet råkar användas på båda datorerna.

På A:

```bash
export BACKFLASH_CONFIG="$PWD/test-mesh-a.toml"
export XDG_DATA_HOME="$PWD/.test-mesh-a-data"
```

På B:

```bash
export BACKFLASH_CONFIG="$PWD/test-mesh-b.toml"
export XDG_DATA_HOME="$PWD/.test-mesh-b-data"
```

Skapa först en tillfällig konfiguration på varje dator med mesh aktiverad,
lyssnare och utan peers:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = []
peer_key = ""
```

Starta `./backflash` en gång och avsluta med `q`. Detta skapar den lokala,
separata BACKFLASH-identiteten. Kör sedan:

```bash
./backflash-cache identity
```

Spara följande värden separat:

```text
publik nyckel:   första hexraden
overlayadress:   raden efter "overlay:"
underlayadress:  datorns nåbara IP eller DNS-namn på port 4242
```

Gör detta på båda datorerna. Kopiera endast publika nycklar och endpoints.
Kopiera aldrig `identity.key`.

## Fas 3: konfigurera peers

Anta följande exempelvärden:

```text
A underlay: 198.51.100.10:4242
B underlay: 198.51.100.20:4242
A nyckel:   <A_KEY>
B nyckel:   <B_KEY>
```

Ändra `test-mesh-a.toml` på A till:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = ["tcp://198.51.100.20:4242"]
peer_key = "<B_KEY>"
```

Ändra `test-mesh-b.toml` på B till:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = ["tcp://198.51.100.10:4242"]
peer_key = "<A_KEY>"
```

Om endast A har en publik/NAT-forwardad endpoint kan B ansluta utgående till
A. För att båda dashboard-vyerna ska visa en ansluten fjärrpeer behöver även
A kunna nå B:s underlay-endpoint.

Tillåt endast den port som används av Yggdrasil-underlayet. Kontrollera först
att porten faktiskt lyssnar:

```bash
ss -ltnp | grep 4242
nc -vz <andra-datorns-underlay-IP> 4242
```

Starta därefter BACKFLASH på båda:

```bash
./backflash
```

Öppna `m` och kontrollera:

```text
Yggdrasil   PÅ
Peers       1
```

`PÅ` betyder att transporten faktiskt är igång. `Peers` räknas från aktiv
Yggdrasil-status, inte från konfigurerade adresser.

Headless verifiering:

```bash
./backflash-cache
```

Den visar `RUNNING` när en peer är aktiv och `DEGRADED` när transporten kör
men ingen peer är ansluten.

## Fas 4: skapa objekt X på A

På A, med origin tillgänglig:

1. Starta `./backflash`.
2. Tryck `f` och öppna ett riktigt Flashback-forum.
3. Öppna en tråd och vänta tills dess första sida visas.
4. Notera tråd-ID:t, till exempel `t1234567`.

När första sidan sparats finns ett publikt objekt med resurs-ID:

```text
flashback / t1234567:1 / thread_page_snapshot
```

Objektet ligger i A:s separata mesh-object store, inte i GANDR och inte som
en del av någon offentlig användarprofil.

På B kan forum-/trådmetadata laddas medan origin är tillgänglig, men öppna inte
tråden ännu. B ska inte redan ha objektet `t1234567:1`.

## Fas 5: verifiera A → B utan origin

Det headless kommandot `get` använder endast mesh-runtime och hämtar inte från
Flashback. På B kör:

```bash
./backflash-cache get flashback t1234567:1 thread_page_snapshot
```

Förväntat resultat:

```text
provenans: PEER_ONLY
källa: flashback
resurs: t1234567:1
```

Detta bevisar följande verkliga flöde:

```text
B lokal miss
  → Yggdrasil till A
  → A serverar publikt objekt
  → B verifierar SHA-256
  → B sparar objektet
  → provenance = PEER_ONLY
```

Kontrollera även objektet lokalt på B:

```bash
grep -R 'PEER_ONLY' "$XDG_DATA_HOME/backflash/mesh/objects"
```

Ingen SQLite-fil, HTML-cache eller objektfil får kopieras mellan datorerna.

## Fas 6: verifiera lokal persistens efter att A stoppats

Stoppa A:

```bash
Ctrl-C
```

På B kör igen:

```bash
./backflash-cache get flashback t1234567:1 thread_page_snapshot
```

Det ska lyckas även när A är avstängd. Objektet hämtas då från B:s lokala
object store. Öppna därefter `./backflash` på B och läs samma tråd; den första
sidan ska kunna visas lokalt.

## Fas 7: identitet över omstart

På respektive dator:

```bash
./backflash-cache identity
```

Notera nyckel och overlayadress, stoppa processen, starta igen och kör samma
kommando. Nyckeln ska vara identisk på samma dator och olika mellan A och B.

Identiteten finns under:

```text
$XDG_DATA_HOME/backflash/mesh/identity.key
```

Den är inte en GANDR-identitet och får inte ersättas med en GANDR-nyckel.

## Negativa kontroller

Kontrollera följande i testloggen och statusvyn:

- fel peer-key ger `ERROR`, inte falsk `PÅ`
- avstängd peer ger timeout/retry och lämnar TUI användbar
- `share_cache = false` serverar inte objekt från noden
- fel SHA-256 avvisas före persistence
- samma objekt kan läsas efter omstart utan ny nätverksförfrågan
- `Ctrl-C` stänger runtime utan att hänga kvar
- `go test ./...` innehåller redan tester för fel hash, duplicate/singleflight,
  persistent identitet, disabled mesh och två-nodsöverföring

Meshobjekt får endast innehålla publika, kanoniska cachedata. Följande lämnar
inte BACKFLASH-meshprotokollet:

```text
Flashback-cookies och credentials
lokal sökhistorik och reader state
Polisen-filter/lokation
GANDR-identitet, nycklar, meddelanden och petnames
```

## Efter testet

Ta bort de tillfälliga testfilerna när du är klar:

```bash
rm -f test-mesh-a.toml test-mesh-b.toml
rm -rf .test-mesh-a-data .test-mesh-b-data
```

Ta inte bort riktiga `~/.local/share/backflash/mesh`-kataloger om deras
identitet eller lokala cache ska behållas.
