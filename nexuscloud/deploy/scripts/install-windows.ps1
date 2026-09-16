<#
.SYNOPSIS
  Termina la instalación de NexusCloud en Windows: directorios, binario,
  config.yaml, migraciones, servicio de Windows, y (si hay credenciales de
  administrador, por variable de entorno o preguntadas aquí) crea el
  primer usuario y arranca el servicio de verdad.

.DESCRIPTION
  Invocado por install.bat (que ya compiló el binario) -- vive en un
  fichero real en vez de generarse línea a línea dentro del .bat porque la
  lógica de aquí (bucles de validación, SecureString, ramas condicionales)
  ya no encaja bien en "un echo por línea" (el propio install.bat avisa de
  ese problema en su cabecera para casos más simples que este).

  Credenciales de administrador: si NX_ADMIN_USERNAME y NX_ADMIN_PASSWORD
  ya vienen fijadas por variable de entorno, se usan sin preguntar nada
  (camino para aprovisionar sin intervención humana). Si no, y hay una
  consola interactiva real por delante (nunca si la entrada está
  redirigida) y no se pidió -Unattended, se preguntan aquí. La contraseña
  nunca se pasa como argumento de linea de comandos a `admin
  create-user` (visible para otros procesos vía la lista de procesos) --
  se le manda por stdin, el mismo camino que ya soporta
  internal/cli/helpers.go:promptPassword cuando stdin no es un terminal.

.PARAMETER BinPath
  Ruta al nexuscloud.exe ya compilado (en una carpeta temporal).
#>
param(
    [switch]$WithWeb,
    [switch]$Unattended,
    [int]$Port = 8080,
    [Parameter(Mandatory = $true)][string]$BinPath
)
$ErrorActionPreference = 'Stop'

$InstallDir = Join-Path $env:ProgramFiles 'NexusCloud'
$ConfigDir  = Join-Path $env:ProgramData  'NexusCloud'
$DataDir    = Join-Path $env:ProgramData  'NexusCloud\data'
$exe        = Join-Path $InstallDir 'nexuscloud.exe'
$configFile = Join-Path $ConfigDir  'config.yaml'

Write-Host "==> Directorios"
New-Item -ItemType Directory -Force -Path $InstallDir, $ConfigDir, $DataDir | Out-Null

Write-Host "==> Binario -> $exe"
Copy-Item -Path $BinPath -Destination $exe -Force

Write-Host "==> Configuracion"
if (Test-Path $configFile) {
    Write-Host "    $configFile ya existe, no se toca"
} else {
    & $exe config init --out $configFile

    if ($WithWeb) {
        # config init no tiene una bandera para esto -- se activa aqui
        # sobre el YAML ya generado. Solo toca la linea "enabled:" que va
        # INMEDIATAMENTE despues de "web:" (un unico campo en esa
        # sección), no cualquier otra que tambien tenga "enabled".
        $lines = Get-Content $configFile
        for ($i = 0; $i -lt $lines.Count; $i++) {
            if ($lines[$i] -eq 'web:' -and $i + 1 -lt $lines.Count -and $lines[$i + 1] -match 'enabled: false') {
                $lines[$i + 1] = '  enabled: true'
            }
        }
        Set-Content $configFile $lines
    }

    if ($Port -ne 8080) {
        # A diferencia de "web:", "server:" tiene varios campos y "port"
        # no es el primero (host va antes) -- hace falta buscar la linea
        # del puerto en si. Sin anclar la indentacion exacta (probado real
        # en Linux contra un config.yaml generado de verdad: yaml.v3 lo
        # escribe con 4 espacios, no 2 -- anclar "^  port: 8080$" con 2
        # asumidos nunca hace match y falla en silencio). "8080" solo
        # aparece una vez en todo config.yaml, un simple substring basta.
        (Get-Content $configFile) -replace 'port: 8080', "port: $Port" | Set-Content $configFile
    }

    # Bug real preexistente, encontrado probando de verdad el equivalente
    # en Linux (no algo que introduzca este cambio): "config init" no
    # tiene ninguna bandera para fijar storage.dataDir, asi que SIEMPRE
    # escribia el default que calcula el propio binario
    # (%ProgramData%\NexusCloud, SIN el "\data" que anade $DataDir aqui
    # arriba) -- el servicio de Windows de verdad leeria esa carpeta, no
    # la que este script preparo, aunque los directorios se hubieran
    # creado en el sitio correcto. A diferencia de Linux (donde el
    # default del script y el del binario suelen coincidir), aqui SIEMPRE
    # son distintos por diseno, asi que esto se aplica siempre, no solo
    # cuando alguien pide algo distinto del default.
    (Get-Content $configFile) -replace 'dataDir: .*', "dataDir: $DataDir" | Set-Content $configFile

    Write-Host "    revisa $configFile antes de arrancar (docs/security.md)"
}

Write-Host "==> Migraciones de base de datos"
$env:NEXUSCLOUD_DATA_DIR = $DataDir
& $exe --config $configFile migrate up

Write-Host "==> Servicio de Windows"
& $exe service install --config $configFile

# ---- Administrador + arranque -----------------------------------------
function ConvertFrom-SecureStringPlain {
    param([Security.SecureString]$Secure)
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToGlobalAllocUnicode($Secure)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringUni($ptr)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeGlobalAllocUnicode($ptr)
    }
}

$adminUser = $env:NX_ADMIN_USERNAME
$adminPass = $env:NX_ADMIN_PASSWORD
$haveCreds = -not [string]::IsNullOrEmpty($adminUser) -and -not [string]::IsNullOrEmpty($adminPass)

if (-not $haveCreds -and -not $Unattended -and -not [Console]::IsInputRedirected) {
    Write-Host ""
    Write-Host "==> Unas pocas preguntas antes de terminar (Enter acepta el valor por defecto)"

    $ans = Read-Host "Nombre de usuario para el administrador [admin]"
    if ([string]::IsNullOrWhiteSpace($ans)) { $ans = 'admin' }
    $adminUser = $ans

    while ($true) {
        $sec1 = Read-Host "Contrasena para ese usuario (minimo 8 caracteres)" -AsSecureString
        $plain1 = ConvertFrom-SecureStringPlain $sec1
        if ($plain1.Length -lt 8) {
            Write-Host "    demasiado corta -- necesita al menos 8 caracteres."
            continue
        }
        $sec2 = Read-Host "Repite la contrasena" -AsSecureString
        $plain2 = ConvertFrom-SecureStringPlain $sec2
        if ($plain1 -ne $plain2) {
            Write-Host "    no coincide con la anterior, vuelve a intentarlo."
            continue
        }
        $adminPass = $plain1
        break
    }
    $haveCreds = $true
}

$webMsg = if ($WithWeb) {
    "incluida y activada (web.enabled: true)"
} else {
    "NO incluida (binario sin ese codigo; web.enabled: false) -- para anadirla despues, nexuscloud\deploy\scripts\enable-web.ps1"
}

if ($haveCreds) {
    Write-Host "==> Creando el administrador '$adminUser'"
    $adminPass | & $exe --config $configFile admin create-user --username $adminUser

    Write-Host "==> Arrancando el servicio"
    & $exe service start

    Start-Sleep -Seconds 2
    $healthOk = $false
    for ($i = 0; $i -lt 5; $i++) {
        try {
            $resp = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/health" -UseBasicParsing -TimeoutSec 2
            if ($resp.StatusCode -eq 200) { $healthOk = $true; break }
        } catch {
            Start-Sleep -Seconds 1
        }
    }

    Write-Host ""
    if ($healthOk) {
        $ip = (Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object { $_.IPAddress -notmatch '^(127\.|169\.254\.)' } |
            Select-Object -First 1).IPAddress
        Write-Host "NexusCloud esta funcionando."
        Write-Host "  Abre esto en tu navegador:  http://localhost:$Port"
        if ($ip) { Write-Host "  O desde otro equipo de tu red:  http://${ip}:$Port" }
        Write-Host "  Usuario:  $adminUser   (la contrasena es la que has elegido)"
    } else {
        Write-Host "NexusCloud se instalo, pero el servicio no respondio a tiempo en http://127.0.0.1:$Port/health."
        Write-Host "  Revisa que paso:  Get-Service nexuscloud   /   el Visor de eventos de Windows"
        Write-Host "  Diagnostico completo:  & '$exe' --config '$configFile' doctor"
    }
} else {
    Write-Host ""
    Write-Host "NexusCloud instalado. El servicio NO se ha arrancado todavia."
    Write-Host "  1. Revisa   $configFile"
    Write-Host "  2. Crea un admin:  & '$exe' --config '$configFile' admin create-user"
    Write-Host "  3. Arranca:  & '$exe' service start   (o desde services.msc)"
}

Write-Host ""
Write-Host "Interfaz web: $webMsg"
Write-Host ""
Write-Host "Actualizar:   nexuscloud\deploy\scripts\update.ps1 <ruta-al-exe-nuevo>"
Write-Host "Desinstalar:  nexuscloud\deploy\scripts\uninstall.ps1   (-Purge borra tambien los datos)"
