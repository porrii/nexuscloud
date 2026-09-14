<#
.SYNOPSIS
  Desinstala NexusCloud (instalación nativa en Windows). Por seguridad NO
  borra datos ni configuración salvo que se pase -Purge.

.EXAMPLE
  .\uninstall.ps1            # quita servicio y binario, conserva datos
  .\uninstall.ps1 -Purge     # además borra datos y configuración
#>
[CmdletBinding()]
param(
    [switch]$Purge,
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'NexusCloud'),
    [string]$ConfigDir  = (Join-Path $env:ProgramData  'NexusCloud')
)
$ErrorActionPreference = 'Stop'

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$p  = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "hay que ejecutar este script como Administrador."
}

$exe = Join-Path $InstallDir 'nexuscloud.exe'

if (Test-Path $exe) {
    Write-Host "==> Parando y desinstalando el servicio"
    & $exe service stop 2>$null
    & $exe service uninstall 2>$null
}

Write-Host "==> Quitando el binario"
Remove-Item -Recurse -Force -Path $InstallDir -ErrorAction SilentlyContinue

if ($Purge) {
    Write-Host "==> -Purge: borrando datos y configuración"
    Remove-Item -Recurse -Force -Path $ConfigDir -ErrorAction SilentlyContinue
    Write-Host "    NexusCloud eliminado por completo."
} else {
    Write-Host "    Servicio y binario eliminados."
    Write-Host "    Se CONSERVA la configuración y los datos en: $ConfigDir"
    Write-Host "    Para borrarlos también:  .\uninstall.ps1 -Purge"
}
