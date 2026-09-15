# Cliente de escritorio (Windows / Linux)

El cliente de escritorio (Flutter, código en `nexuscloud/client/`)
sincroniza una o varias carpetas locales con tu servidor NexusCloud
automáticamente — la alternativa a subir/bajar archivos a mano por CLI o
por la web cada vez.

## Instalar

- **Windows**: instalador MSIX (ver los releases del repositorio), o
  compilar desde código: `flutter build windows` dentro de
  `nexuscloud/client/`.
- **Linux**: compilar desde código: `flutter build linux` dentro de
  `nexuscloud/client/`. (Necesitas el SDK de Flutter instalado; no lo
  gestiona `install.sh`, que es solo para el servidor.)

## Primer login

Al abrir la app por primera vez, pide la URL de tu servidor (p.ej.
`https://tu-servidor.ejemplo:8080` o `http://192.168.1.10:8080` en tu
LAN), tu usuario y contraseña — las mismas credenciales que usarías por
CLI o por la web. Si tu usuario tiene doble factor activado (ver
[`administracion.md`](administracion.md#doble-factor-totp)), pedirá
también el código de tu app autenticadora.

## Configurar la sincronización

Desde la pantalla de sincronización, cada **par** que configures liga una
carpeta remota (dentro de tu NexusCloud) con una carpeta local de tu
equipo, con su propio sentido:

- **Descargar**: servidor → local. Nunca sube ni borra nada en el
  servidor — el modo más seguro si solo quieres tener una copia local de
  lo que ya subiste desde otro lado.
- **Subir**: local → servidor. Nunca descarga ni borra localmente; si
  subes una versión nueva de un archivo que ya existía, el servidor
  conserva la anterior como versión (no se pierde nada, ver
  `nexuscloud/docs/storage.md` §15).
- **Ambos**: reconcilia los dos lados en cada pasada. Si el mismo archivo
  cambió en los dos sitios desde la última sincronización, NexusCloud
  nunca sobrescribe en silencio — crea una *copia de conflicto* con los
  dos contenidos a salvo, para que decidas tú cuál te quedas.

Puedes tener **varios pares sincronizándose a la vez** (p.ej. `/Documentos`
en un sentido y `/Fotos` en otro), y activar o desactivar la sincronización
automática de cada par por separado sin tener que quitarlo de la lista.

## Sincronización automática y en segundo plano

- Con la sincronización automática activada, la app revisa cambios al
  intervalo que configures, sin que tengas que darle a "Sincronizar"
  cada vez.
- También reacciona al momento a cambios en tus carpetas locales
  (vigilancia del sistema de archivos), no solo al reloj.
- Cerrar la ventana **minimiza a la bandeja del sistema** en vez de cerrar
  la app — la sincronización sigue funcionando en segundo plano. Para
  cerrarla de verdad, usa la opción "Salir" del icono de la bandeja.
- En Windows, puedes marcar que la app arranque sola al iniciar sesión
  (opcionalmente ya minimizada) — así la sincronización queda activa
  incluso después de reiniciar el equipo, sin que nadie tenga que abrirla
  a mano.

## Papelera y versiones, también desde el cliente

- Un archivo borrado localmente durante una sincronización en modo
  "Ambos"/"Descargar" pasa a una **papelera local** (distinta de la
  papelera del servidor) — recuperable desde la propia app.
- Compartir archivos/carpetas (crear, gestionar, revocar) y ver lo que
  otros han compartido contigo se hace también desde el cliente, con
  paridad completa frente a la interfaz web.

## Descargas grandes e interrumpidas

Las descargas soportan reanudación (HTTP Range): si se corta la conexión
a mitad de un archivo grande, la siguiente sincronización continúa desde
donde se quedó en vez de empezar de cero.

## Si algo no sincroniza

- Comprueba que el servidor responde: `nexuscloud --config config.yaml doctor`
  desde el propio servidor, o simplemente abrir la URL del servidor en un
  navegador.
- Una copia de conflicto (modo "Ambos") no es un error — es la señal de
  que dos cambios independientes necesitan que decidas cuál conservar.
- Para dudas generales de uso del servidor (no del cliente en sí), ver
  [`preguntas-frecuentes.md`](preguntas-frecuentes.md).
