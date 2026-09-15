<#
.SYNOPSIS
  Actualiza una instalación nativa de NexusCloud (Windows) a un binario
  nuevo, sin destruir datos (NEXUSCLOUD.md §59): para el servicio (si
  estaba corriendo), sustituye el binario (guardando el anterior), aplica
  migraciones y vuelve a arrancar. Equivalente Windows de
  nexuscloud/deploy/scripts/update.sh (Linux) -- mismo comportamiento,
  gap de paridad detectado durante la Parte B de la auditoría "todo por
  comandos" (2026-09-15): hasta ahora solo existía uninstall.ps1.

.EXAMPLE
  .\update.ps1 C:\ruta\a\nexuscloud-nuevo.exe
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$BinSrc,
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'NexusCloud'),
    [string]$ConfigDir  = (Join-Path $env:ProgramData  'NexusCloud')
)
$ErrorActionPreference = 'Stop'

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$p  = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "hay que ejecutar este script como Administrador."
}

if (-not (Test-Path $BinSrc)) { throw "no encuentro el binario nuevo en: $BinSrc" }
$target = Join-Path $InstallDir 'nexuscloud.exe'
$configFile = Join-Path $ConfigDir 'config.yaml'
if (-not (Test-Path $configFile)) { throw "no encuentro $configFile -- ¿está NexusCloud instalado?" }

$oldVer = & $target version 2>&1
$newVer = & $BinSrc version 2>&1
Write-Host "==> $oldVer  ->  $newVer"

$svc = Get-Service -Name nexuscloud -ErrorAction SilentlyContinue
$wasRunning = ($svc -and $svc.Status -eq 'Running')
if ($wasRunning) {
    Write-Host "==> Parando el servicio"
    & $target service stop
}

$backup = "$target.bak-$(Get-Date -Format yyyyMMddHHmmss)"
Write-Host "==> Copia de seguridad del binario -> $backup"
Copy-Item -Path $target -Destination $backup -Force

Write-Host "==> Instalando el binario nuevo"
Copy-Item -Path $BinSrc -Destination $target -Force

Write-Host "==> Migraciones de base de datos"
& $target --config $configFile migrate up
if ($LASTEXITCODE -ne 0) {
    Write-Warning "migrate up falló: restaurando el binario anterior"
    Copy-Item -Path $backup -Destination $target -Force
    if ($wasRunning) { & $target service start }
    exit 1
}

if ($wasRunning) {
    Write-Host "==> Arrancando el servicio"
    & $target service start
    Start-Sleep -Seconds 1
    $svc2 = Get-Service -Name nexuscloud -ErrorAction SilentlyContinue
    if ($svc2 -and $svc2.Status -eq 'Running') {
        Write-Host "    OK"
    } else {
        throw "el servicio no arrancó; revisa el Visor de eventos o 'nexuscloud service status'."
    }
} else {
    Write-Host "==> El servicio estaba parado; no se arranca."
}
Write-Host "Actualización completada."
