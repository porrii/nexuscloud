@echo off
setlocal enabledelayedexpansion
REM Instala NexusCloud en Windows: compila desde codigo fuente (instalando
REM Go si hace falta) y lo registra como servicio de Windows. Prioridad
REM menor que install.sh (Linux) -- mismo criterio.
REM
REM Uso:
REM   install.bat        (consola de Administrador) -- compila sin la interfaz web
REM   install.bat /web   (consola de Administrador) -- igual, pero incluye tambien la web
REM
REM /web es opt-in a proposito (secure/lean by default, mismo criterio que
REM install.sh en Linux): sin el, el binario embebe solo un placeholder
REM vacio, y web.enabled queda en "false" en el config.yaml generado. Para
REM anadir la web mas adelante a una instalacion ya hecha, sin reinstalar
REM desde cero: nexuscloud\deploy\scripts\enable-web.ps1
REM
REM Nota de estilo: usa goto en vez de bloques if/else multilinea a
REM proposito -- un bloque "( ... )" que contiene lineas con parentesis
REM propios (como el codigo PowerShell que genera mas abajo) es una fuente
REM clasica de errores de parseo en cmd.exe; goto evita ese problema por
REM completo, cada linea se interpreta por separado.
REM
REM Para actualizar o desinstalar mas adelante, usa
REM nexuscloud\deploy\scripts\update.ps1 / uninstall.ps1.

set "MIN_GO_VERSION=1.25"
set "MIN_NODE_MAJOR=20"
set "SCRIPT_DIR=%~dp0"
set "CODE_DIR=%SCRIPT_DIR%nexuscloud"

set "WITH_WEB=0"
if /i "%~1"=="/web" set "WITH_WEB=1"

net session >nul 2>&1
if not "%ERRORLEVEL%"=="0" goto :err_not_admin

if not exist "%CODE_DIR%" goto :err_no_code_dir

echo ==^> Comprobando Go
where go >nul 2>&1
if "%ERRORLEVEL%"=="0" goto :go_found

echo     no encontrado -- instalando Go %MIN_GO_VERSION%+ (descarga oficial de go.dev)
set "GOINSTALL_PS1=%TEMP%\nexuscloud-goinstall-%RANDOM%.ps1"
> "%GOINSTALL_PS1%" echo $ErrorActionPreference = 'Stop'
>>"%GOINSTALL_PS1%" echo $v = (Invoke-RestMethod 'https://go.dev/VERSION?m=text').Split("`n")[0]
>>"%GOINSTALL_PS1%" echo $arch = 'amd64'
>>"%GOINSTALL_PS1%" echo if (-not [Environment]::Is64BitOperatingSystem) { $arch = '386' }
>>"%GOINSTALL_PS1%" echo $url = "https://go.dev/dl/$v.windows-$arch.msi"
>>"%GOINSTALL_PS1%" echo $msi = Join-Path $env:TEMP 'nexuscloud-go-install.msi'
>>"%GOINSTALL_PS1%" echo Write-Host "    descargando $url..."
>>"%GOINSTALL_PS1%" echo Invoke-WebRequest -Uri $url -OutFile $msi
>>"%GOINSTALL_PS1%" echo Write-Host '    instalando (silencioso)...'
>>"%GOINSTALL_PS1%" echo Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart" -Wait
>>"%GOINSTALL_PS1%" echo Remove-Item $msi -Force
powershell -NoProfile -ExecutionPolicy Bypass -File "%GOINSTALL_PS1%"
set "GOINSTALL_RESULT=%ERRORLEVEL%"
del /f /q "%GOINSTALL_PS1%" >nul 2>&1
if not "%GOINSTALL_RESULT%"=="0" goto :err_go_install
set "PATH=%ProgramFiles%\Go\bin;%PATH%"
echo     instalado -- puede que tengas que abrir una consola nueva la proxima vez para que 'go' este en el PATH
goto :build

:go_found
for /f "tokens=3" %%v in ('go version') do set "GOVER=%%v"
echo     encontrado: %GOVER%

if not "%WITH_WEB%"=="1" goto :web_build_skip

echo ==^> Comprobando Node.js
where node >nul 2>&1
if not "%ERRORLEVEL%"=="0" goto :install_node
for /f "tokens=1 delims=v." %%v in ('node --version') do set "NODEVER=%%v"
if %NODEVER% LSS %MIN_NODE_MAJOR% goto :install_node
echo     encontrado: v%NODEVER%.x
goto :web_build

:install_node
echo     no encontrado, o version anterior a v%MIN_NODE_MAJOR% -- instalando Node.js LTS (descarga oficial de nodejs.org)
set "NODEINSTALL_PS1=%TEMP%\nexuscloud-nodeinstall-%RANDOM%.ps1"
> "%NODEINSTALL_PS1%" echo $ErrorActionPreference = 'Stop'
>>"%NODEINSTALL_PS1%" echo if (-not [Environment]::Is64BitOperatingSystem^) { throw 'arquitectura de 32 bits no soportada para instalar Node.js automaticamente -- instalalo a mano desde https://nodejs.org/' }
>>"%NODEINSTALL_PS1%" echo $index = Invoke-RestMethod 'https://nodejs.org/dist/index.json'
>>"%NODEINSTALL_PS1%" echo $ver = ($index ^| Where-Object { $_.lts -ne $false } ^| Select-Object -First 1^).version
>>"%NODEINSTALL_PS1%" echo $url = "https://nodejs.org/dist/$ver/node-$ver-x64.msi"
>>"%NODEINSTALL_PS1%" echo $msi = Join-Path $env:TEMP 'nexuscloud-node-install.msi'
>>"%NODEINSTALL_PS1%" echo Write-Host "    descargando $url..."
>>"%NODEINSTALL_PS1%" echo Invoke-WebRequest -Uri $url -OutFile $msi
>>"%NODEINSTALL_PS1%" echo Write-Host '    instalando (silencioso)...'
>>"%NODEINSTALL_PS1%" echo Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart" -Wait
>>"%NODEINSTALL_PS1%" echo Remove-Item $msi -Force
powershell -NoProfile -ExecutionPolicy Bypass -File "%NODEINSTALL_PS1%"
set "NODEINSTALL_RESULT=%ERRORLEVEL%"
del /f /q "%NODEINSTALL_PS1%" >nul 2>&1
if not "%NODEINSTALL_RESULT%"=="0" goto :err_node_install
set "PATH=%ProgramFiles%\nodejs;%PATH%"
echo     instalado -- puede que tengas que abrir una consola nueva la proxima vez para que 'node'/'npm' esten en el PATH

:web_build
echo ==^> Compilando la interfaz web (nexuscloud\web)
pushd "%CODE_DIR%\web"
call npm ci --no-audit --no-fund
if not "%ERRORLEVEL%"=="0" goto :err_npm
call npm run build
if not "%ERRORLEVEL%"=="0" goto :err_npm
popd
echo     web compilada
goto :build

:web_build_skip
REM Sin /web, dist\ debe quedar reducida al placeholder de siempre, sin
REM importar que hubiera antes (p.ej. restos de un "npm run build" manual
REM anterior, o de una instalacion previa con /web en este mismo checkout)
REM -- go:embed empaqueta lo que encuentre en el disco al compilar, no lo
REM que "deberia" haber segun ningun flag. web\embed.go (go:embed all:dist)
REM tambien exige que quede AL MENOS un fichero, de ahi el .gitkeep.
for /d %%d in ("%CODE_DIR%\web\dist\*") do rd /s /q "%%d" >nul 2>&1
del /f /q "%CODE_DIR%\web\dist\*" >nul 2>&1
type nul > "%CODE_DIR%\web\dist\.gitkeep"

:build
echo ==^> Compilando nexuscloud.exe (puede tardar la primera vez)
set "BUILD_DIR=%TEMP%\nexuscloud-build-%RANDOM%"
mkdir "%BUILD_DIR%"
pushd "%CODE_DIR%"
set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w" -o "%BUILD_DIR%\nexuscloud.exe" .\cmd\nexuscloud
if not "%ERRORLEVEL%"=="0" goto :err_build
popd
echo     binario listo

REM ---- Instalar como servicio (antes delegado en un
REM nexuscloud\deploy\scripts\install.ps1 separado -- unificado aqui en un
REM unico script, mismo criterio que install.sh en Linux). Generado como
REM .ps1 temporal en vez de -Command en linea, por el mismo motivo que el
REM paso de Go de arriba: evita mezclar el quoting de batch con el de
REM PowerShell en una sola linea larga.
echo ==^> Instalando el servicio
set "INSTALL_PS1=%TEMP%\nexuscloud-install-%RANDOM%.ps1"
> "%INSTALL_PS1%" echo $ErrorActionPreference = 'Stop'
>>"%INSTALL_PS1%" echo $InstallDir = Join-Path $env:ProgramFiles 'NexusCloud'
>>"%INSTALL_PS1%" echo $ConfigDir  = Join-Path $env:ProgramData  'NexusCloud'
>>"%INSTALL_PS1%" echo $DataDir    = Join-Path $env:ProgramData  'NexusCloud\data'
>>"%INSTALL_PS1%" echo $exe        = Join-Path $InstallDir 'nexuscloud.exe'
>>"%INSTALL_PS1%" echo $configFile = Join-Path $ConfigDir  'config.yaml'
>>"%INSTALL_PS1%" echo Write-Host "==^> Directorios"
>>"%INSTALL_PS1%" echo New-Item -ItemType Directory -Force -Path $InstallDir, $ConfigDir, $DataDir ^| Out-Null
>>"%INSTALL_PS1%" echo Write-Host "==^> Binario -^> $exe"
>>"%INSTALL_PS1%" echo Copy-Item -Path '%BUILD_DIR%\nexuscloud.exe' -Destination $exe -Force
>>"%INSTALL_PS1%" echo Write-Host "==^> Configuracion"
>>"%INSTALL_PS1%" echo if (Test-Path $configFile^) {
>>"%INSTALL_PS1%" echo     Write-Host "    $configFile ya existe, no se toca"
>>"%INSTALL_PS1%" echo } else {
>>"%INSTALL_PS1%" echo     ^& $exe config init --out $configFile
if "%WITH_WEB%"=="1" goto :ps1_enable_web
goto :ps1_config_done
:ps1_enable_web
REM config init no tiene una bandera para esto -- se activa aqui sobre el
REM YAML ya generado, mismo criterio que la unit de systemd en Linux (sed
REM sobre una plantilla ya escrita). Solo toca la linea "enabled:" que va
REM INMEDIATAMENTE despues de "web:", no cualquier otra seccion que tambien
REM tenga un campo "enabled".
>>"%INSTALL_PS1%" echo     $lines = Get-Content $configFile
>>"%INSTALL_PS1%" echo     for ($i = 0; $i -lt $lines.Count; $i++) { if ($lines[$i] -eq 'web:' -and $i + 1 -lt $lines.Count -and $lines[$i+1] -match 'enabled: false') { $lines[$i+1] = '  enabled: true' } }
>>"%INSTALL_PS1%" echo     Set-Content $configFile $lines
:ps1_config_done
>>"%INSTALL_PS1%" echo     Write-Host "    revisa $configFile antes de arrancar (docs/security.md)"
>>"%INSTALL_PS1%" echo }
>>"%INSTALL_PS1%" echo Write-Host "==^> Migraciones de base de datos"
>>"%INSTALL_PS1%" echo $env:NEXUSCLOUD_DATA_DIR = $DataDir
>>"%INSTALL_PS1%" echo ^& $exe --config $configFile migrate up
>>"%INSTALL_PS1%" echo Write-Host "==^> Servicio de Windows"
>>"%INSTALL_PS1%" echo ^& $exe service install --config $configFile
>>"%INSTALL_PS1%" echo Write-Host ""
>>"%INSTALL_PS1%" echo Write-Host "NexusCloud instalado. El servicio NO se ha arrancado todavia."
>>"%INSTALL_PS1%" echo Write-Host "  1. Revisa   $configFile"
>>"%INSTALL_PS1%" echo Write-Host "  2. Crea un admin:  ^&$exe --config $configFile admin create-user"
>>"%INSTALL_PS1%" echo Write-Host "  3. Arranca:  ^&$exe service start   (o desde services.msc)"
>>"%INSTALL_PS1%" echo Write-Host ""
set "WEBMSG=NO incluida (binario sin ese codigo; web.enabled: false) -- para anadirla despues, nexuscloud\deploy\scripts\enable-web.ps1"
if "%WITH_WEB%"=="1" set "WEBMSG=incluida y activada (web.enabled: true)"
>>"%INSTALL_PS1%" echo Write-Host "Interfaz web: %WEBMSG%"
>>"%INSTALL_PS1%" echo Write-Host ""
>>"%INSTALL_PS1%" echo Write-Host "Actualizar:   nexuscloud\deploy\scripts\update.ps1 <ruta-al-exe-nuevo>"
>>"%INSTALL_PS1%" echo Write-Host "Desinstalar:  nexuscloud\deploy\scripts\uninstall.ps1   (-Purge borra tambien los datos)"
powershell -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_PS1%"
set "INSTALL_RESULT=%ERRORLEVEL%"
del /f /q "%INSTALL_PS1%" >nul 2>&1
rmdir /s /q "%BUILD_DIR%" >nul 2>&1
exit /b %INSTALL_RESULT%

:err_not_admin
echo install.bat: hay que ejecutarlo como Administrador (clic derecho -^> "Ejecutar como administrador").
exit /b 1

:err_no_code_dir
echo install.bat: no encuentro la carpeta nexuscloud\ junto a este script -- ^¿lo estas ejecutando desde la raiz del repo clonado?
exit /b 1

:err_go_install
echo install.bat: fallo instalando Go automaticamente -- instalalo a mano desde https://go.dev/dl/ y vuelve a lanzar este script.
exit /b 1

:err_build
popd
echo install.bat: go build fallo.
exit /b 1

:err_node_install
echo install.bat: fallo instalando Node.js automaticamente -- instalalo a mano desde https://nodejs.org/ y vuelve a lanzar este script (o sin /web para seguir sin la web).
exit /b 1

:err_npm
popd
echo install.bat: npm ci / npm run build fallo compilando la interfaz web.
exit /b 1
