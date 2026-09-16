@echo off
setlocal enabledelayedexpansion
REM Instala NexusCloud en Windows: compila desde codigo fuente (instalando
REM Go si hace falta) y lo registra como servicio de Windows. Prioridad
REM menor que install.sh (Linux) -- mismo criterio.
REM
REM Uso normal (interactivo): clic derecho -> "Ejecutar como
REM administrador" sobre este fichero. Con una consola real por delante,
REM el paso final (nexuscloud\deploy\scripts\install-windows.ps1) pregunta
REM lo poco que hace falta (usuario y contrasena del administrador) y deja
REM NexusCloud funcionando de verdad -- admin creado, servicio arrancado,
REM sin ningun paso manual mas.
REM
REM Uso avanzado / scriptado (sin preguntas):
REM   install.bat /unattended     -- comportamiento clasico: sin admin, sin arrancar
REM   set NX_ADMIN_USERNAME=admin
REM   set NX_ADMIN_PASSWORD=...
REM   install.bat                 -- crea admin y arranca sin preguntar nada
REM   install.bat /web            -- incluye tambien la interfaz web
REM
REM /web es opt-in a proposito (secure/lean by default, mismo criterio que
REM install.sh en Linux) EN EL CAMINO SCRIPTADO -- en el camino
REM interactivo (install-windows.ps1) se pregunta con "si" por defecto,
REM porque sin ella no hay forma de usar NexusCloud sin la linea de
REM comandos. Sin la web (ni por pregunta ni por /web), el binario embebe
REM solo un placeholder vacio, y web.enabled queda en "false". Para
REM anadirla mas adelante a una instalacion ya hecha, sin reinstalar desde
REM cero: nexuscloud\deploy\scripts\enable-web.ps1
REM
REM Nota de estilo: usa goto en vez de bloques if/else multilinea a
REM proposito -- un bloque "( ... )" que contiene lineas con parentesis
REM propios (como el codigo PowerShell que genera mas abajo) es una fuente
REM clasica de errores de parseo en cmd.exe; goto evita ese problema por
REM completo, cada linea se interpreta por separado. Por el mismo motivo,
REM la parte final (preguntas + admin + arranque, con bucles de
REM validacion) vive en un fichero .ps1 real
REM (nexuscloud\deploy\scripts\install-windows.ps1) en vez de generarse
REM linea a linea como el resto de este fichero -- esa logica ya no
REM encaja bien en "un echo por linea".
REM
REM Para actualizar o desinstalar mas adelante, usa
REM nexuscloud\deploy\scripts\update.ps1 / uninstall.ps1.

set "MIN_GO_VERSION=1.25"
set "MIN_NODE_MAJOR=20"
set "SCRIPT_DIR=%~dp0"
set "CODE_DIR=%SCRIPT_DIR%nexuscloud"
if not defined NX_PORT set "NX_PORT=8080"

set "WITH_WEB=0"
set "UNATTENDED=0"
for %%A in (%*) do (
    if /i "%%A"=="/web" set "WITH_WEB=1"
    if /i "%%A"=="/unattended" set "UNATTENDED=1"
)

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

REM ---- Instalar como servicio, preguntar admin y arrancar: vive en un
REM fichero .ps1 real (nexuscloud\deploy\scripts\install-windows.ps1),
REM no generado linea a linea -- ver la nota de estilo de la cabecera.
echo ==^> Instalando el servicio
set "PS_SWITCHES="
if "%WITH_WEB%"=="1" set "PS_SWITCHES=%PS_SWITCHES% -WithWeb"
if "%UNATTENDED%"=="1" set "PS_SWITCHES=%PS_SWITCHES% -Unattended"
powershell -NoProfile -ExecutionPolicy Bypass -File "%CODE_DIR%\deploy\scripts\install-windows.ps1" -Port %NX_PORT% -BinPath "%BUILD_DIR%\nexuscloud.exe"%PS_SWITCHES%
set "INSTALL_RESULT=%ERRORLEVEL%"
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
