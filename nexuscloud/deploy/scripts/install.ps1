<#
.SYNOPSIS
  Instalación nativa de NexusCloud en Windows como servicio del sistema.
  No usa Docker.

.DESCRIPTION
  Copia el binario a "Archivos de programa", genera la configuración en
  ProgramData, aplica migraciones y registra el servicio de Windows vía
  `nexuscloud service install`. Hay que ejecutarlo como Administrador.

.PARAMETER BinPath
  Ruta a nexuscloud.exe. Por defecto: .\nexuscloud.exe junto a este script.

.EXAMPLE
  # Desde una consola elevada:
  .\install.ps1 -BinPath .\nexuscloud.exe
#>
[CmdletBinding()]
param(
    [string]$BinPath      = (Join-Path $PSScriptRoot 'nexuscloud.exe'),
    [string]$InstallDir   = (Join-Path $env:ProgramFiles 'NexusCloud'),
    [string]$ConfigDir    = (Join-Path $env:ProgramData  'NexusCloud'),
    [string]$DataDir      = (Join-Path $env:ProgramData  'NexusCloud\data')
)
$ErrorActionPreference = 'Stop'

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p  = New-Object Security.Principal.WindowsPrincipal($id)
    if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "hay que ejecutar este script como Administrador."
    }
}

Assert-Admin
if (-not (Test-Path $BinPath)) { throw "no encuentro el binario en: $BinPath" }

$exe        = Join-Path $InstallDir 'nexuscloud.exe'
$configFile = Join-Path $ConfigDir  'config.yaml'

Write-Host "==> Directorios"
New-Item -ItemType Directory -Force -Path $InstallDir, $ConfigDir, $DataDir | Out-Null

Write-Host "==> Binario -> $exe"
Copy-Item -Path $BinPath -Destination $exe -Force

Write-Host "==> Configuración"
if (Test-Path $configFile) {
    Write-Host "    $configFile ya existe, no se toca"
} else {
    & $exe config init --out $configFile
    # NEXUSCLOUD_DATA_DIR se fija en la definición del servicio de abajo.
    Write-Host "    revisa $configFile antes de arrancar (docs/security.md)"
}

Write-Host "==> Migraciones de base de datos"
$env:NEXUSCLOUD_DATA_DIR = $DataDir
& $exe --config $configFile migrate up

Write-Host "==> Servicio de Windows"
& $exe service install --config $configFile

Write-Host ""
Write-Host "NexusCloud instalado. El servicio NO se ha arrancado todavía."
Write-Host "  1. Revisa   $configFile"
Write-Host "  2. Crea un admin:  `"$exe`" --config `"$configFile`" admin create-user"
Write-Host "  3. Arranca:  `"$exe`" service start   (o desde services.msc)"
Write-Host ""
Write-Host "Desinstalar:  .\uninstall.ps1   (añade -Purge para borrar datos)"
