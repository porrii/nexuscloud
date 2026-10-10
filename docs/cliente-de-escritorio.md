# Cliente de escritorio (Windows / Linux)

El cliente de escritorio (Flutter, código en `nexuscloud/client/`)
sincroniza una o varias carpetas locales con tu servidor NexusCloud
automáticamente — la alternativa a subir/bajar archivos a mano por CLI o
por la web cada vez.

## Instalar

- **Windows**: instalador `NexusCloud-win-Setup.exe` (ver los releases
  del repositorio), o compilar desde código:
  `nexuscloud/deploy/scripts/package-client-windows.ps1`. Al no estar
  firmado, la primera vez que lo ejecutes Windows SmartScreen puede
  avisar — "Más información" → "Ejecutar de todas formas".
- **Linux**: compilar desde código: `flutter build linux` dentro de
  `nexuscloud/client/`. (Necesitas el SDK de Flutter instalado; no lo
  gestiona `install.sh`, que es solo para el servidor.) La vista previa
  de vídeo y audio usa libmpv: instala `libmpv-dev` (Debian/Ubuntu) o el
  paquete equivalente de tu distribución antes de compilar. Si al
  ejecutar falta libmpv, la app funciona igual y solo esa vista previa
  avisa de que no está disponible.

## Actualizar (Windows)

El propio cliente comprueba si hay una versión nueva al abrirse (sin
descargar nada solo) y avisa con la etiqueta **Nueva** junto a "Ajustes"
en la barra lateral. En Ajustes → Actualizaciones, "Descargar" y luego
"Reiniciar y actualizar" — la app se cierra, se actualiza y se vuelve a
abrir sola, sin instalador aparte.

Esto requiere que tu servidor tenga activada la comprobación de
actualizaciones (`clientUpdates.enabled: true` en `config.yaml`, ver
[`administracion.md`](administracion.md)) — si no, nunca aparece ningún
aviso, sin que sea un error.

## Primer login

Al abrir la app por primera vez, pide la URL de tu servidor (p.ej.
`https://tu-servidor.ejemplo:8080` o `http://192.168.1.10:8080` en tu
LAN), tu usuario y contraseña — las mismas credenciales que usarías por
CLI o por la web. Si tu usuario tiene doble factor activado (ver
[`administracion.md`](administracion.md#doble-factor-totp)), pedirá
también el código de tu app autenticadora. La próxima vez recordará la
URL del servidor.

## La ventana

A la izquierda, una barra lateral fija con el buscador y las secciones:
**Mis archivos**, **Compartido**, **Sincronización**, **Papelera** y
**Ajustes** (y **Administración** si tu cuenta es de administrador, ver
más abajo). Abajo, siempre visibles, el estado de la sincronización
("Todo sincronizado", "Sincronizando…", "Requiere tu revisión"), el
espacio usado frente a tu cuota y tu usuario con "Cerrar sesión". Las
subidas y descargas en curso se ven en un panel en la parte inferior,
estés en la sección que estés.

En **Mis archivos**:

- Arrastra archivos o carpetas enteras desde el Explorador de Windows
  para subirlos a la carpeta actual, o usa "Subir".
- **Vista previa**: doble clic (o Intro) sobre un archivo lo abre dentro
  de la app sin descargarlo: imágenes (con zoom), PDF, vídeo, audio,
  Markdown, código y texto. Con ← → (o RePág/AvPág) pasas al archivo
  anterior o siguiente de la carpeta. Lo que no se puede previsualizar,
  o es demasiado grande (texto de más de 5 MB, imágenes de más de 64 MB
  o de 40 megapíxeles, PDF de más de 100 MB), ofrece "Descargar". Un
  "vídeo" que por dentro es texto (una lista de reproducción disfrazada)
  no se reproduce, por seguridad. Vídeo y audio se previsualizan en los
  formatos habituales (MP4/MOV, MKV/WebM, MP3, OGG, FLAC, WAV, AAC); otros,
  como AVI, hay que descargarlos (ver ADR-046).
- "Nueva carpeta", y en cada elemento (al pasar el ratón, con clic
  derecho o con "Más acciones"): vista previa, descargar, compartir,
  renombrar, mover a otra carpeta, historial de versiones y mover a la
  papelera.
- Selección múltiple con Ctrl+clic y Mayús+clic, para descargar, mover o
  borrar varios a la vez.
- Vista de lista o de cuadrícula (la aplicación recuerda la que elegiste),
  orden por nombre, tamaño o fecha (clic en la cabecera de la columna) y
  un filtro rápido de la carpeta actual. Para buscar en **todas** tus
  carpetas, usa el buscador de la barra lateral (Ctrl+K).
- En la cuadrícula, las imágenes, los vídeos y los PDF muestran su
  miniatura si el servidor tiene activadas las miniaturas; si no, se ve
  el icono del tipo de archivo.

Atajos de teclado: Ctrl+K buscar · Ctrl+1…6 cambiar de sección · Ctrl+U
subir · Ctrl+N nueva carpeta · Ctrl+F filtrar · F2 renombrar · Supr
papelera · Ctrl+A seleccionar todo · Retroceso o Alt+↑ subir un nivel ·
F5 actualizar · Mayús+F10 menú contextual. La lista completa está en
Ajustes.

## Administración (solo administradores)

Si tu cuenta es de administrador, la barra lateral muestra
**Administración** (Ctrl+6), con cinco pestañas:

- **Usuarios**: buscar, crear cuentas (con rol, y una contraseña que
  puedes generar y copiar; lo copiado se borra del portapapeles al
  minuto), editar nombre, correo y cuota, desactivar o
  activar, añadir a un grupo y eliminar. Eliminar pide escribir el
  nombre de usuario y es irreversible (ver
  [`administracion.md`](administracion.md#borrar-un-usuario-para-siempre));
  ante la duda, desactiva. Tu propia cuenta no se puede desactivar ni
  eliminar desde aquí.
- **Grupos**: crear grupos y fijar su cuota por miembro.
- **Invitaciones**: crear (rol, número de usos, caducidad) y revocar. El
  código solo se muestra una vez, al crearla (y, si lo copias, también se
  borra del portapapeles al minuto).
- **Auditoría**: los eventos del servidor, del más reciente al más
  antiguo, con un filtro de "Solo alertas" (accesos fallidos y errores).
- **Sistema**: discos del servidor y cola de miniaturas.

Desactivar una cuenta cierra sus sesiones y anula sus tokens; con un
servidor actualizado también deja de servir sus enlaces públicos y de
subida anónima (ver
[`administracion.md`](administracion.md)), y reactivarla lo restaura todo.
El rol «Solo lectura» aún no se ofrece desde la app: un servidor anterior
no lo aplicaría y la app no puede saber qué versión tiene el servidor.

Todavía se hace solo por CLI: cambiar el rol o la contraseña de otra
cuenta, ver los miembros de un grupo o sacar a alguien de él, sesiones y
tokens de otros usuarios, backups, Storage Pools y la configuración del
servidor.

## Configurar la sincronización

En la sección **Sincronización**, "Vincular carpeta" crea un **par** que
liga una carpeta remota (dentro de tu NexusCloud) con una carpeta local de
tu equipo, con su propio sentido. Cada par aparece como una tarjeta con su
último resultado (descargados, subidos, conflictos, errores) y sus
acciones:

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

Todo esto se configura en **Ajustes**:

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
  papelera del servidor) — recuperable desde la propia app, en Papelera
  → pestaña "En este equipo" (la del servidor está en la pestaña "En el
  servidor").
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
