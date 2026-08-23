# BACKFLASH – prestandabaslinje

Mätningarna görs mot lokal SQLite-cache och utan Flashback-nätverk i
dashboardfrågan.

## Uppmätt

På Linux amd64, Intel i7-10510U:

```text
BenchmarkDashboardSnapshotCached: cirka 0,54 ms/op
Yggdrasil tvånods-cacheöverföring: cirka 3,06 s
```

Dashboard-snapshoten använder en samlad SQL-aggregatfråga och laddar inte hela
posthistoriken. Yggdrasilvärdet inkluderar lokal peer-linketablering och ska
inte jämföras med cached läsning.

## Profilering

Kör:

```bash
BACKFLASH_PROFILE=1 backflash
```

Profileringen skriver endast tidsmätningar till stderr. Den loggar inte
cookies, sökningar, privata nycklar eller foruminnehåll.

## Begränsningar

Första målningen i den interaktiva terminalen behöver fortfarande mätas med en
TTY-harness. Nätverk får inte flyttas in i första målningen; lokalt innehåll
ska visas först och uppdateras asynkront.
