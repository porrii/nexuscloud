<#
.SYNOPSIS
  Añade la interfaz web a una instalación nativa de NexusCloud (Windows) YA
  HECHA sin ella (install.bat sin /web, el caso por defecto). En un solo
  paso: Node.js si falta -> compila la web -> recompila el binario con la
  web real embebida -> activa web.enabled en el config.yaml real -> deja
  que update.ps1 (parar/backup/instalar/migrar/reiniciar) haga el resto.

  La dirección inversa ("quitar la web") no necesita un script propio: ya
  funciona con lo que existe -- vuelve a compilar con el install.bat normal
  de la raíz (sin /web) y pasa ese .exe a update.ps1, exactamente igual que
  aquí pero sin el paso de Node/npm.

.EXAMPLE
  .\enable-web.ps1
#>
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'NexusCloud'),
    [string]$ConfigDir  = (Join-Path $env:ProgramData  'NexusCloud'),
    [int]$MinNodeMajor = 20
)
$ErrorActionPreference = 'Stop'

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$p  = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "hay que ejecutar este script como Administrador."
}

$configFile = Join-Path $ConfigDir 'config.yaml'
if (-not (Test-Path $configFile)) { throw "no encuentro $configFile -- ¿está NexusCloud instalado?" }

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$codeDir = Split-Path -Parent (Split-Path -Parent $scriptDir)
if (-not (Test-Path (Join-Path $codeDir 'web'))) {
    throw "no encuentro $codeDir\web -- ¿este script sigue dentro de nexuscloud\deploy\scripts\ del repo clonado?"
}

Write-Host "==> Comprobando Node.js"
$needNode = $true
$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if ($nodeCmd) {
    $verStr = (& node --version).TrimStart('v')
    $major = [int]($verStr.Split('.')[0])
    if ($major -ge $MinNodeMajor) {
        $needNode = $false
        Write-Host "    Node.js v$verStr ya instalado (>= v$MinNodeMajor), no hace falta reinstalar"
    }
}

if ($needNode) {
    Write-Host "    Instalando Node.js (no encontrado, o versión anterior a v$MinNodeMajor)"
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw "arquitectura de 32 bits no soportada para instalar Node.js automáticamente -- instálalo a mano desde https://nodejs.org/"
    }
    $index = Invoke-RestMethod 'https://nodejs.org/dist/index.json'
    $ver = ($index | Where-Object { $_.lts -ne $false } | Select-Object -First 1).version
    $url = "https://nodejs.org/dist/$ver/node-$ver-x64.msi"
    $msi = Join-Path $env:TEMP 'nexuscloud-node-install.msi'
    Write-Host "    descargando $url..."
    Invoke-WebRequest -Uri $url -OutFile $msi
    Write-Host "    instalando (silencioso)..."
    Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart" -Wait
    Remove-Item $msi -Force
    $env:PATH = "$env:ProgramFiles\nodejs;$env:PATH"
    Write-Host "    instalado en $env:ProgramFiles\nodejs"
}

Write-Host "==> Compilando la interfaz web ($codeDir\web)"
Push-Location (Join-Path $codeDir 'web')
try {
    npm ci --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw "npm ci falló" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build falló" }
} finally {
    Pop-Location
}

Write-Host "==> Comprobando Go"
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd -and (Test-Path (Join-Path $env:ProgramFiles 'Go\bin\go.exe'))) {
    # install.bat, si tuvo que instalar Go, lo deja en Program Files\Go sin
    # persistir el PATH -- este script suele correr en una sesión de shell
    # NUEVA, así que hay que mirar aquí también antes de darlo por ausente.
    $env:PATH = "$env:ProgramFiles\Go\bin;$env:PATH"
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
}
if (-not $goCmd) { throw "no encuentro 'go' en el PATH -- instálalo o añádelo al PATH y vuelve a lanzar este script." }

Write-Host "==> Compilando nexuscloud.exe con la web embebida"
$buildDir = Join-Path $env:TEMP "nexuscloud-build-$(Get-Random)"
New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
Push-Location $codeDir
try {
    $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags "-s -w" -o (Join-Path $buildDir 'nexuscloud.exe') .\cmd\nexuscloud
    if ($LASTEXITCODE -ne 0) { throw "go build falló" }
} finally {
    Pop-Location
}
Write-Host "    binario listo"

Write-Host "==> Activando web.enabled en $configFile"
$lines = Get-Content $configFile
$hasWebSection = $lines -contains 'web:'
if ($hasWebSection) {
    # Solo toca la línea "enabled:" que va INMEDIATAMENTE después de "web:"
    # -- no cualquier otra sección que también tenga un campo "enabled". Si
    # ya estaba en "true" (segunda ejecución), esto no encuentra nada que
    # cambiar y Set-Content simplemente reescribe el fichero sin tocarlo.
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -eq 'web:' -and $i + 1 -lt $lines.Count -and $lines[$i+1] -match 'enabled: false') {
            $lines[$i+1] = '  enabled: true'
        }
    }
    Set-Content $configFile $lines
} else {
    # config.yaml de antes de que "web" existiera como sección (no debería
    # pasar hoy, config init siempre la incluye, pero por si viene de una
    # instalación muy antigua editada a mano).
    Add-Content $configFile "`nweb:`n  enabled: true"
}

Write-Host "==> Instalando el binario nuevo (parar/backup/migrar/reiniciar vía update.ps1)"
& (Join-Path $scriptDir 'update.ps1') (Join-Path $buildDir 'nexuscloud.exe') -InstallDir $InstallDir -ConfigDir $ConfigDir

Write-Host "Interfaz web activada."
