# Installera BACKFLASH på Windows

BACKFLASH kan köras lokalt utan konfiguration. Installationsskriptet kopierar
Windows-binären, skapar lokala datakataloger och skriver en säker
mesh-konfiguration där mesh är avstängt.

Öppna PowerShell i katalogen där binären och skriptet ligger:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-backflash-windows.ps1 -Binary .\backflash-windows-amd64.exe
```

Skriptet frågar innan det skapar en brandväggsregel. Regeln gäller endast TCP
4242 på Windows-profilerna `Private` och `Domain`. Porten behövs bara om du
vill att andra BACKFLASH-noder ska kunna ansluta till din mesh-peer. Den ger
inte BACKFLASH tillgång till Flashback och öppnar inte GANDR.

För en installation utan brandväggsfråga:

```powershell
.\install-backflash-windows.ps1 -Binary .\backflash-windows-amd64.exe -SkipFirewall
```

För att skapa regeln direkt måste PowerShell köras som administratör:

```powershell
.\install-backflash-windows.ps1 -Binary .\backflash-windows-amd64.exe -OpenFirewall
```

## Filer

```text
Binär:          %LOCALAPPDATA%\Backflash\backflash.exe
Konfiguration:  %USERPROFILE%\.config\backflash\config.toml
Lokal data:     %USERPROFILE%\.local\share\backflash\
```

Mesh är opt-in. Ett minimalt exempel efter att en legitim peer har erhållits:

```toml
[mesh]
enabled = true
share_cache = true
listen = ["tcp://0.0.0.0:4242"]
peers = ["tcp://PEER-ADRESS:4242"]
peer_key = "PEERNS-64-TECKEN-LÅNGA-PUBLIKA-NYCKEL"
```

`peer_key` är en publik mesh-nyckel i hexformat. Klistra aldrig in en privat
`identity.key` i konfigurationen. BACKFLASH-meshens identitet är separat från
GANDR och Flashback-sessioner.

Om du inte använder mesh behövs ingen brandväggsregel och ingen handskriven
konfiguration.
