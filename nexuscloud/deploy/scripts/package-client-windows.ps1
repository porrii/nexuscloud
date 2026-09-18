<#
.SYNOPSIS
  Compila el cliente de escritorio de Windows y lo empaqueta con Velopack
  (ADR-032): instalador (Setup.exe) + paquete completo + releases.win.json,
  listos para publicar. Sustituye al empaquetado MSIX (retirado) -- MSIX no
  puede auto-actualizarse sin un certificado de firma real.

.DESCRIPTION
  1. `flutter build windows --release`, inyectando la versión de
     pubspec.yaml como APP_VERSION (--dart-define) -- la misma versión que
     compara lib/features/update/ contra el feed, y la misma que se le pasa
     a `vpk pack --packVersion`.
  2. `vpk pack` sobre esa build ya compilada.

  Requiere `flutter` y la herramienta `vpk` (`dotnet tool install -g vpk`)
  en el PATH. Si `vpk` falla con "hostfxr.dll not found" pese a tener el
  SDK de .NET instalado (visto en un install de .NET en una ruta no
  estándar), fija $env:DOTNET_ROOT a la carpeta de tu instalación de
  .NET antes de ejecutar este script.

.EXAMPLE
  .\package-client-windows.ps1
#>
[CmdletBinding()]
param(
    [string]$OutputDir
)
$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ClientDir = Join-Path (Join-Path $ScriptDir '..\..') 'client' | Resolve-Path

Get-Command flutter -ErrorAction Stop | Out-Null
if (-not (Get-Command vpk -ErrorAction SilentlyContinue)) {
    throw "no encuentro 'vpk' en el PATH -- instala con: dotnet tool install -g vpk"
}

$pubspec = Get-Content (Join-Path $ClientDir 'pubspec.yaml') -Raw
if ($pubspec -notmatch '(?m)^version:\s*([0-9]+\.[0-9]+\.[0-9]+)') {
    throw "no pude leer la versión de pubspec.yaml"
}
$Version = $Matches[1]
Write-Host "==> Versión: $Version"

if (-not $OutputDir) {
    $OutputDir = Join-Path $ClientDir 'Releases'
}

Push-Location $ClientDir
try {
    Write-Host "==> flutter build windows --release"
    flutter build windows --release --dart-define=APP_VERSION=$Version
    if ($LASTEXITCODE -ne 0) { throw "flutter build windows falló" }
} finally {
    Pop-Location
}

$PackDir = Join-Path $ClientDir 'build\windows\x64\runner\Release'
if (-not (Test-Path (Join-Path $PackDir 'nexuscloud_client.exe'))) {
    throw "no encuentro nexuscloud_client.exe en $PackDir -- ¿falló el build?"
}
$Icon = Join-Path $ClientDir 'windows\runner\resources\app_icon.ico'

Write-Host "==> vpk pack (canal win, salida en $OutputDir)"
vpk pack `
    --packId NexusCloud `
    --packVersion $Version `
    --packDir $PackDir `
    --mainExe nexuscloud_client.exe `
    --packTitle NexusCloud `
    --packAuthors Porrii `
    --icon $Icon `
    --outputDir $OutputDir `
    -y
if ($LASTEXITCODE -ne 0) { throw "vpk pack falló" }

Write-Host "==> Listo: $OutputDir"
Write-Host "    De todo lo generado aqui, el proxy del servidor y el cliente solo"
Write-Host "    necesitan estos tres (el resto -- assets.win.json, RELEASES, el"
Write-Host "    Portable.zip -- son metadatos internos de vpk que nadie lee, no los subas):"
Write-Host "      - NexusCloud-$Version-full.nupkg"
Write-Host "      - NexusCloud-win-Setup.exe"
Write-Host "      - releases.win.json"
Write-Host "    Para publicar (gh ya autenticado en esta maquina):"
Write-Host "    gh release create vX.Y.Z `"$OutputDir\NexusCloud-$Version-full.nupkg`" `"$OutputDir\NexusCloud-win-Setup.exe`" `"$OutputDir\releases.win.json`" --repo porrii/nexuscloud --title `"NexusCloud vX.Y.Z`" --notes `"...`""
