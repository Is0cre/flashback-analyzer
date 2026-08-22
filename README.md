# Flashback Analyzer

Read-only grund för att samla in och analysera Flashback-trådar utan att blanda ihop citerad text med nya påståenden.

## Vad v0.1 gör

- tar en `https://www.flashback.org/t<ID>`-URL
- hämtar trådsidor försiktigt med minst 5.2 s mellan nya nätanrop
- cachear HTML lokalt
- parsar post-ID, användare, tid, synligt postnummer, text, citat och länkar
- lagrar allt i SQLite
- sparar rå HTML per trådsida samt källa och innehållshash för varje post
- kan synkronisera den senast kända sidans svans och nya sidor idempotent
- normaliserar länkar och visar domän, unika URL:er och unika länkande användare
- delar trådar deterministiskt i kronologiska analyssegment
- visar deltagarstatistik, topplista, Top-10-andel, Gini och HHI
- har tomma men färdiga tabeller för kommande stance/opinionsanalys

Verktyget är medvetet read-only. Det postar ingenting till Flashback.

## Installation

```bash
cd flashback-analyzer
python -m venv .venv
source .venv/bin/activate
python -m pip install -U pip
pip install -e '.[dev]'
pytest -q
```

## Första körningen

Starta den lokala terminal-läsaren direkt:

```bash
fb
```

Den öppnar redan lagrade trådar utan nätverk. `fb tui t<ID>` är en explicit
alternativ start med en viss tråd.

Hämta första sidan:

```bash
fb ingest 'https://www.flashback.org/t3322511'
```

Hämta exempelvis fem sidor totalt från startsidan:

```bash
fb ingest 'https://www.flashback.org/t3322511' --pages 5
```

Hämta alla sidor som discoveras från första sidan:

```bash
fb ingest 'https://www.flashback.org/t3322511' --all
```

Visa rå parser-kvalitet innan någon AI kopplas in:

```bash
fb inspect t3322511 --limit 20
```

Visa statistik:

```bash
fb stats t3322511
```

Synkronisera endast trådens aktuella slut:

```bash
fb sync t3322511
```

Visa metadata:

```bash
fb thread-info t3322511
```

Visa normaliserad länkanvändning per domän:

```bash
fb links t3322511
```

Bygg och visa analyssegment (75 inlägg som standard):

```bash
fb segments t3322511
```

Segmentgränser och medlemskap lagras separat från rådata i `segments` och
`segment_posts`. Segmentens sammanfattning är avsiktligt tom tills en
versionshanterad analysleverantör införs.

Hitta återkommande ämneskandidater:

```bash
fb topics t3322511
```

Den första implementationen använder en konservativ, reproducerbar lexikal
baslinje. Ämnena är kandidater och ska inte tolkas som färdiga frågor eller
positioner.

Frågekandidater från uttryckliga frågetecken kan visas med:

```bash
fb questions t3742384C
```

Detta lager återger bara frågor som faktiskt skrivits i inläggen. Det fyller
inte i svarsalternativ eller tolkar en fråga som en ståndpunkt.

En liten interaktiv terminalvy finns också:

```bash
fb tui t3742384C
```

`fb` utan subkommando öppnar den lokala Textual-läsaren direkt. Den kräver
ingen nätverksanslutning för att läsa redan lagrade trådar.

Utan trådargument öppnas trådlistan. Där kan du lägga till en ny tråd, ange
antal sidor att hämta eller välja att hämta alla upptäckta sidor:

```bash
fb tui
```

Vid start läser TUI:t också de cacheade/live listorna för aktuella, populära,
nya ämnen och nya inlägg. Välj en rad med dess nummer för att börja hämta
tråden. Tryck `r` i trådlistan för att uppdatera listorna.

Discovery-resultat sparas i SQLite, så den senast kända listan visas även om
forumets live-sidor tillfälligt inte kan hämtas.

Tryck `f` i Textual-läsaren för att bläddra i Flashbacks cacheade forumträd.
`Enter` öppnar kategori/forum, `b` går upp, `t` återgår till sparade trådar och
`r` uppdaterar den aktuella nivån. Forumträdet cacheas i SQLite och visas
offline; en ny tråd hämtas först när den väljs.

Live-listan visar visningar, läsare och svar innan hämtning. TUI:t hämtar en
sida som standard, visar en grov siduppskattning och kräver extra bekräftelse
för att hämta hela trådar med minst 1 000 svar.

TUI:t är kompakt som standard. Visa ANSI-logotypen uttryckligen med:

```bash
fb tui --logo
```

Menyerna navigeras med ↑/↓ och Enter. q eller Escape avslutar en meny; text
behövs bara när du anger tråd eller antal sidor.

The TUI displays the repository ANSI logo from `assets/ansi-art.utf.ans`
when available.

Alla kommandon accepterar korta referenser som `t3742384C`; fullständig
`https://www.flashback.org/`-URL behövs inte.

## Datamodell

```text
threads ──< posts >── users
             │
             ├──< quotes
             ├──< links
             └──< stances >── questions
```

`raw_pages` bevarar hämtad HTML med hash och källa. `posts.content_hash` och
`posts.source_url` gör varje normaliserat inlägg spårbart. `schema_version` och
additiva migreringar gör att äldre SQLite-filer kan öppnas utan att data skrivs
om eller förloras.

Citaten ligger separat. `posts.text` försöker endast innehålla vad skribenten själv skrev, medan `posts.raw_text` behåller hela det renderade meddelandet för felsökning.

## Nästa steg: opinion

Nästa lager bör inte vara vanlig sentimentanalys. Flödet bör vara:

```text
tråd
  → upptäck huvudfrågor/teser
  → klassificera varje relevant post per fråga
  → aggreggera per unik användare
  → aggreggera separat per inlägg
  → argumentkluster
  → opinion över tid
```

Föreslagna stance-värden finns redan i SQLite-schemat:

```text
strong_yes
probably_yes
uncertain
probably_no
strong_no
irrelevant
unclear
```

Det gör att en extremt aktiv användare aldrig automatiskt blir "100 röster".

## Viktigt om parsern

Flashbacks HTML kan ändras. Parsern använder därför flera fallbacks. Kör alltid `fb inspect` på en verklig tråd efter installation och lägg en anonymiserad HTML-fixture i `tests/fixtures/` när du hittar en struktur parsern missar.
