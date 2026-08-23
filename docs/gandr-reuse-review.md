# GANDR och BACKFLASH – återanvändningsgranskning

GANDR är ett separat projekt och förblir separat. Den här granskningen beskriver
tekniska mönster, inte kod- eller identitetsdelning.

## Värt att återanvända

Direkt som idé:

- GANDR:s Bubble Tea-rootmodell och tydliga `Update`/`View`-flöde.
- Responsiva terminalbrytningar: full vy, kompakt vy, mini/cyberdeck och smal vy.
- Lip Gloss-stilar organiserade kring semantiska roller i stället för enskilda vyer.
- En generisk serie/sparkline för aktivitet. BACKFLASH kan använda samma form för
  inlägg/minut, trådar/minut, eventdata och cachetrafik.
- Försiktig reconnect/backoff som mönster för framtida nätverksjobb.

Efter generalisering:

- GANDR:s embedded Yggdrasil-transport kan inspirera ett separat BACKFLASH-
  cachetransportlager. Det ska ha en egen cache-peer-nyckel och ett eget protokoll.
- GANDR:s atomiska content-addressed object store är relevant för publika
  BACKFLASH-cacheobjekt, men måste få BACKFLASH:s provenance- och storleksregler.

## Ska inte kopplas in

Följande är GANDR-domän och ska inte återanvändas av BACKFLASH:

- identitetsnycklar, vault, kontakt- och petname-databas
- Gandr federation, chattprotokoll, privata meddelanden och IPC
- krypterad klientdatabas och social graph
- Gandr-profiler eller identitetsrotation

BACKFLASH får inte korrelera Flashback-användare, Flashback-cookies eller läshistorik
med Gandr-identiteter. En eventuell publik cache-mesh delar endast offentliga,
content-addressed objekt och får inte vara en hemlig kanal.

## Aktuell BACKFLASH-strategi

SQLite är den lokala källan för navigering och läsning. Forum och trådlistor visas
från cache först och uppdateras asynkront när deras sync-state är stale. Nätverk får
inte blockera första vyn. `BACKFLASH_PROFILE=1` visar tidsmätningar för launcher,
databas, navigation, trådlistor, poster och rendering utan att logga innehåll.

GANDR:s SQLite-driver används inte som förebild för ett byte: BACKFLASH använder
redan en CGO-fri SQLite-driver för enklare portabla byggen.

## Faktisk livscykel i GANDR

GANDR:s klient och daemon har olika roller. `cmd/gandr` laddar en krypterad
identitet och öppnar den krypterade klientdatabasen innan den ansluter till
`gandrd` över IPC. `cmd/gandrd` äger nodens livscykel och startar bland annat
embedded Yggdrasil med en separat transportnyckel. Detta är inte samma nyckel
som användaridentiteten.

BACKFLASH importerar därför inte GANDR-modulen i den första integrationen.
`internal/gandr` är en liten låst gräns som kan visa ofarlig status, men öppnar
varken GANDR:s vault, identitet eller privata databas vid startup. En framtida
upplåsning måste återanvända GANDR:s riktiga krypterade lagring genom en
explicit adapter eller IPC-gräns; en parallell BACKFLASH-vault får inte skapas.

## Integritetsinvarianter

Följande ska förbli sant även när subsystemet byggs ut:

- Gandr-identitet och BACKFLASH:s eventuella cache-peer-identitet är olika.
- GANDR:s krypterade klientdatabas och BACKFLASH:s publika SQLite/cache är olika
  lagringsdomäner.
- Gandr-petnames, privata meddelanden och Flashback-läsdata lämnar aldrig sina
  respektive domäner.
- BACKFLASH får inte skicka Flashback-cookies, Flashback-användarnamn eller
  läshistorik till GANDR eller cache-mesh.

Detta skyddar GANDR:s privata kommunikation från oavsiktlig exponering via en
publik klient. Om gränsen bryts kan en publik cache eller dashboard börja
fungera som en identitets- eller metadata-korrelationstjänst, vilket är ett
arkitekturfel och inte bara en UI-fråga.
