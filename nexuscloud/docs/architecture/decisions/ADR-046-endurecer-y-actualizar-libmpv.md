# ADR-046: Endurecer y actualizar libmpv en el cliente de escritorio

## Estado

Aceptado (2026-10-10). Dirección decidida por el usuario: endurecer, actualizar
y documentar el riesgo residual. Afecta a la vista previa de vídeo y audio
del cliente de escritorio (§35). Relacionado con ADR-010 (descargas con
`Range`) y ADR-041 (miniaturas, el otro punto donde se procesa contenido de
terceros, en el servidor).

## Contexto

La vista previa de vídeo y audio del cliente usa media_kit, que envuelve
libmpv (y, dentro, FFmpeg). Esos vídeos pueden venir de terceros: subida
anónima (§38), enlaces y carpetas compartidas. Además, quien los abre suele
ser un administrador.

Revisión de seguridad del 2026-10-09. Comprobado después con la libmpv real
(prueba temporal, no versionada), no solo leyendo el código:

1. **Robo del token de sesión.** media_kit aplica `Media.httpHeaders` como
   la propiedad GLOBAL `http-header-fields` de mpv, y FFmpeg la reenvía a
   todo lo que abre. Un «.mp4» que en realidad es una lista HLS (o una M3U
   simple) con segmentos en otro host hizo que ese host recibiera
   `Authorization: Bearer …` (7 veces con HLS, 1 con M3U). Ya está
   corregido en el cliente: el reproductor habla con un proxy de loopback
   (`core/network/authorized_stream_proxy.dart`) que añade el token solo
   hacia el recurso exacto del servidor, y antes de abrir se rechaza un
   «medio» cuyo primer KB es texto. Comprobado: con el proxy, el host
   atacante recibe la petición sin `Authorization`.
2. **FFmpeg antiguo.** media_kit_libs_windows_video 1.0.11 trae fija la
   build `mpv-dev-x86_64-20230924` (verificada solo con MD5), con un FFmpeg
   sin los parches de seguridad posteriores a septiembre de 2023, y
   media_kit acepta por defecto `allowed_extensions=ALL` y cualquier
   demuxer.
3. **Dependabot no ve esa DLL**: la descarga el CMake del paquete, no está
   en `pubspec.lock`.

## Decisión

Tres capas, todas en el cliente (el servidor no cambia):

1. **Que el reproductor nunca tenga el token** (ya aplicado): proxy de
   loopback (puerto aleatorio, ruta secreta de 256 bits comparada en tiempo
   constante, solo GET/HEAD, solo reenvía `Range`, sin redirecciones) y
   rechazo de los «medios» que son texto (`looksLikeTextNotMedia`).
2. **Endurecer mpv** (`kHardenedMpvOptions` en
   `features/files/presentation/preview/media_preview.dart`, más
   `PlayerConfiguration(protocolWhitelist: ['http','tcp'])`):
   - `demuxer-lavf-o=protocol_whitelist=[http,tcp],format_whitelist=[mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,mp3,ogg,flac,wav,aac]`:
     FFmpeg solo habla con el proxy y solo abre esos demuxers.
   - `ytdl=no`, `load-unsafe-playlists=no`, `ordered-chapters=no`,
     `sub-auto=no`, `audio-file-auto=no`, `cover-art-auto=no`,
     `cache-on-disk=no`.

   Verificado: mp4, mkv, webm y mp3 se reproducen, y avi se rechaza
   («Failed to recognize file format»).

   **Descartados por haberlos probado**: `demuxer=lavf` y
   `access-references=no` rompen media_kit, que abre cada medio a través de
   una lista temporal local («Unable to load playlist …»). Antes de añadir
   otra opción, repite la batería de formatos.
3. **Actualizar libmpv**: `client/windows/CMakeLists.txt` descarga
   `mpv-dev-x86_64-20261010-git-b2c255c13e.7z` de
   shinchiro/mpv-winbuild-cmake, lo verifica con el SHA-256 que GitHub
   publica para el asset (`EXPECTED_HASH`) e instala su `libmpv-2.dll`
   después de las bibliotecas de los plugins, así que sustituye a la de
   media_kit tanto en `flutter run` como en release.
   `deploy/scripts/package-client-windows.ps1` fija además el SHA-256 de la
   DLL resultante y de `pdfium.dll`, y detiene la release si no coinciden.

   Se usa la variante x86_64 básica, no la `-v3`, que exige AVX2.

   La build nueva pasó la misma batería: formatos, rechazo de avi y proxy
   sin token. El renderizado en ventana (media_kit_video con la API de
   render de mpv) solo se comprueba a mano en la app.

## Consecuencias

- El instalador de Windows crece: la libmpv nueva ocupa ~120 MB sin
  comprimir, frente a ~29 MB de la de 2023.
- **Actualizar libmpv es manual**, cada pocos meses o ante un CVE de
  FFmpeg:
  1. Elegir una release de shinchiro/mpv-winbuild-cmake y tomar el SHA-256
     del asset `mpv-dev-x86_64-*.7z` (`gh api …/releases/latest`).
  2. Cambiar nombre, URL y hash en `client/windows/CMakeLists.txt`.
  3. Compilar en release, calcular con `Get-FileHash` el hash de
     `libmpv-2.dll` y actualizarlo en el script de empaquetado.
  4. Repetir la batería: mp4, mkv, webm y mp3 se reproducen, avi se
     rechaza, y una HLS o M3U disfrazada no lleva el token.
  5. Probar a mano un vídeo en la app.
- Si se actualiza media_kit, revisa que siga funcionando la sustitución, es
  decir, que nuestra `install()` vaya después de `PLUGIN_BUNDLED_LIBRARIES`.
- **Linux** usa la libmpv del sistema (`libmpv2`), que se actualiza con la
  distribución. El endurecimiento de la capa 2 aplica igual. Si falta
  libmpv, la app arranca y solo la vista previa de vídeo avisa
  (inicialización perezosa).
- **Riesgo residual aceptado**: los demuxers y decoders permitidos (mov,
  matroska, mp3…) siguen procesando contenido de terceros sin aislamiento
  de proceso, igual que cualquier reproductor de escritorio. La lista
  blanca reduce la superficie, no la elimina. Aislar el reproductor en un
  proceso aparte queda como posible mejora futura.
- La lista temporal que media_kit usa para abrir medios impide un
  endurecimiento más agresivo sin parchear media_kit.
