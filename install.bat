@echo off
setlocal enabledelayedexpansion
REM Instala NexusCloud en Windows: compila desde codigo fuente (instalando
REM Go si hace falta) y lo registra como servicio de Windows. Prioridad
REM menor que install.sh (Linux) -- mismo criterio, envoltorio fino sobre
REM lo que ya existe: nexuscloud\deploy\scripts\install.ps1.
REM
REM Uso: ejecuta este .bat desde una consola de Administrador (clic derecho
REM       -> "Ejecutar como administrador").
REM
REM Nota de estilo: usa goto en vez de bloques if/else multilinea a
REM proposito -- un bloque "( ... )" que contiene lineas con parentesis
REM propios (como el codigo PowerShell que genera mas abajo) es una fuente
REM clasica de errores de parseo en cmd.exe; goto evita ese problema por
REM completo, cada linea se interpreta por separado.

set "MIN_GO_VERSION=1.25"
set "SCRIPT_DIR=%~dp0"
set "CODE_DIR=%SCRIPT_DIR%nexuscloud"

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

REM La interfaz web (nexuscloud\web\) NO se compila aqui a proposito: exige
REM Node.js ademas de Go, y viene desactivada por defecto (secure-by-default)
REM -- si la quieres, sigue el paso manual del README tras esta instalacion.

echo ==^> Instalando el servicio
powershell -NoProfile -ExecutionPolicy Bypass -File "%CODE_DIR%\deploy\scripts\install.ps1" -BinPath "%BUILD_DIR%\nexuscloud.exe"
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
