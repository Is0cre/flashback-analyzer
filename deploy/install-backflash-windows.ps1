<#
BACKFLASH-installationsskript för Windows.

Skriptet installerar en redan byggd backflash.exe lokalt. Det installerar
inte GANDR, importerar inga nycklar och aktiverar inte mesh automatiskt.
#>

[CmdletBinding()]
param(
    [string]$Binary = "",
    [string]$InstallDir = "$env:LOCALAPPDATA\Backflash",
    [int]$MeshPort = 4242,
    [switch]$SkipFirewall,
    [switch]$OpenFirewall
)

$ErrorActionPreference = "Stop"

function Write-Info([string]$Message) {
    Write-Host "BACKFLASH: $Message" -ForegroundColor Cyan
}

function Is-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if (-not $Binary) {
    $candidates = @(
        (Join-Path (Get-Location) "backflash-windows-amd64.exe"),
        (Join-Path (Get-Location) "backflash.exe")
    )
    $Binary = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}

if (-not $Binary -or -not (Test-Path $Binary -PathType Leaf)) {
    throw "Hittade ingen Windows-binär. Kör med -Binary .\backflash-windows-amd64.exe"
}

if ($MeshPort -lt 1 -or $MeshPort -gt 65535) {
    throw "Meshporten måste ligga mellan 1 och 65535."
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$target = Join-Path $InstallDir "backflash.exe"
Copy-Item -Force $Binary $target

# BACKFLASH använder dessa kataloger på Windows också, eftersom de följer
# samma portabla XDG-liknande layout som Linux/macOS.
$configDir = Join-Path $env:USERPROFILE ".config\backflash"
$dataDir = Join-Path $env:USERPROFILE ".local\share\backflash"
New-Item -ItemType Directory -Force -Path $configDir, $dataDir | Out-Null
$configPath = Join-Path $configDir "config.toml"

if (-not (Test-Path $configPath)) {
    @"
# BACKFLASH lokal konfiguration
#
# Mesh är avstängt som standard. Slå bara på det om du vill delta i det
# publika cache-nätet och har konfigurerat en legitim peer.

[mesh]
enabled = false
share_cache = false
listen = ["tcp://0.0.0.0:$MeshPort"]
peers = []
peer_key = ""
"@ | Set-Content -Encoding UTF8 $configPath
    Write-Info "Skapade konfiguration: $configPath"
} else {
    Write-Info "Behåller befintlig konfiguration: $configPath"
}

Write-Info "Installerade binär: $target"
Write-Info "Lokal data sparas under: $dataDir"

if (-not $SkipFirewall) {
    $firewallChoice = $OpenFirewall
    if (-not $OpenFirewall) {
        Write-Host ""
        Write-Host "BACKFLASH mesh är frivilligt och avstängt som standard." -ForegroundColor Yellow
        Write-Host "Om mesh aktiveras behöver andra BACKFLASH-noder kunna nå TCP-port $MeshPort." 
        Write-Host "Regeln öppnar endast den porten för Windows-profilerna Private och Domain."
        Write-Host "Den öppnar inte Flashback, GANDR eller någon annan port."
        $answer = Read-Host "Skapa brandväggsregel för BACKFLASH mesh? [j/N]"
        $firewallChoice = $answer -match "^(j|ja|y|yes)$"
    }

    if ($firewallChoice) {
        if (-not (Is-Administrator)) {
            Write-Warning "Brandväggsregeln kräver PowerShell som administratör. Kör om med 'Kör som administratör' eller skapa regeln manuellt."
        } else {
            $ruleName = "BACKFLASH public cache mesh (TCP $MeshPort)"
            Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
            New-NetFirewallRule -DisplayName $ruleName `
                -Direction Inbound -Action Allow -Protocol TCP -LocalPort $MeshPort `
                -Profile Domain,Private -Description "Tillåter inkommande BACKFLASH-cachepeers. Endast publika cacheobjekt delas." | Out-Null
            Write-Info "Öppnade TCP-port $MeshPort för BACKFLASH mesh (Private/Domain)."
        }
    } else {
        Write-Info "Ingen brandväggsregel skapad. BACKFLASH fungerar fortfarande lokalt/offline."
    }
}

Write-Host ""
Write-Info "Starta med: $target"
Write-Info "Mesh-konfiguration: $configPath"
Write-Host "Aktivera inte mesh förrän peer-adress och peer_key är ifyllda." -ForegroundColor Yellow
