# BACKFLASH cache-mesh runtime

BACKFLASH:s mesh är en frivillig, publik cachefunktion. Den är inte en
central cache, inte ett arkiv och inte en auktoritet för Flashback-innehåll.
Lokalt innehåll och Flashbacks origin-data har företräde.

## Identitet och domäner

När mesh aktiveras skapas en separat Yggdrasil-transportnyckel i:

```text
$XDG_DATA_HOME/backflash/mesh/identity.key
```

Nyckeln skapas inte i disabled-läge och är aldrig härledd från Gandr,
Flashback-cookie/session, användarnamn, hostname, MAC-adress eller annan
operativsystemsidentitet. Gandr har egen identitet, egen lagring och eget
protokoll.

## Konfiguration

Den nuvarande konfigurationsläsaren använder explicita miljövariabler:

```bash
BACKFLASH_MESH_ENABLED=1
BACKFLASH_MESH_SHARE_CACHE=1
BACKFLASH_MESH_LISTEN=tcp://127.0.0.1:4242
BACKFLASH_MESH_PEERS=tcp://peer.example:4242
BACKFLASH_MESH_PEER_KEY=<hexadecimal Yggdrasil-publik nyckel>
```

Mesh är avstängd som standard. `enabled` startar transporten; `share_cache`
avgör om den får svara på GET för publika cacheobjekt. En nod kan alltså
hämta från peers utan att dela sin lokala cache.

## Runtime-state

```text
DISABLED    mesh är avstängd
CONFIGURED  mesh är vald men inte startad
STARTING    identitet/transport initieras
RUNNING     transporten är aktiv och en peer är ansluten
DEGRADED    transporten är aktiv men ingen användbar peer är ansluten
STOPPING    avstängning pågår
ERROR       transporten kunde inte starta eller stannade oväntat
```

`RUNNING` sätts aldrig enbart för att konfigurationen säger enabled.

## Delade objekt

Endast verifierade, publika, content-addressed cacheobjekt kan serveras.
Cookies, sessioner, läshistorik, sökningar, reader-state, petnames, Gandr-data
och privata nycklar ligger utanför mesh-object store.

Peerobjekt verifieras med SHA-256 före lagring och märks `PEER_ONLY`.
Bootstrap-peers används bara för anslutning; de är inte centrala sannings- eller
cachekällor. Etablerade peers ska kunna fortsätta utan någon central nod.
