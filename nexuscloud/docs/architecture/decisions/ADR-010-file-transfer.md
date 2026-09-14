# ADR-010: Subida y descarga en el cliente Flutter — un solo picker, streaming a disco, y un fix real al interceptor de reintentos

## Estado

Aceptado.

## Contexto

El slice 1 del cliente Desktop (ADR-009) dejó explícitamente fuera de alcance subir y descargar archivos. Este slice los añade sobre el explorador de archivos ya existente (`client/lib/features/files/`).

`ADR-009` ya justificó `dio` sobre `package:http` precisamente por esto ("necesario para progreso de subida/descarga en un slice futuro") — este ADR no repite ese razonamiento, solo documenta lo nuevo.

Antes de escribir código se investigó cómo resuelve esto el cliente web (`web/src/pages/FilesPage.tsx`/`client.ts`) para no inventar una UX distinta, y cómo usa NexusKeys `file_selector`/`file_picker`. Una validación posterior contra el código fuente real de Dio 5.11.1 encontró un problema real en el diseño inicial antes de implementarlo (ver decisión 3).

## Decisión

1. **Un solo paquete de selección de archivos: `file_selector`, no también `file_picker`.** NexusKeys usa `file_picker` para guardar/exportar porque `file_selector.getSaveLocation()` no existe en Android — pero este cliente solo apunta a Windows/Linux en este slice (Android es Fase 4), y `getSaveLocation()` sí existe en ambas. Añadir la segunda dependencia ahora resolvería un problema que todavía no se tiene (§162). Advertencia conocida y sin resolver: hay un issue abierto de Flutter (`flutter/flutter#137072`) sobre comportamiento irregular de `getSaveLocation()` en Linux, que NexusKeys nunca ejercita (no distribuye ahí). La interfaz de dominio (`FilesRepository`) no conoce `file_selector` en absoluto — está confinado a `presentation/pages/file_browser_page.dart` — así que si el picker de guardado necesita cambiarse a `file_picker` más adelante, el cambio no toca dominio ni datos.
2. **Streaming a disco en ambas direcciones, no todo en memoria.** Descarga: `dio.download(url, rutaLocal, onReceiveProgress:...)`, que escribe directo a disco. Subida: el archivo local se lee como `Stream<List<int>>` (`file.openRead()`), no con `readAsBytes()`. `Content-Length` se declara explícitamente en la petición de subida porque, verificado en el código fuente de Dio, sin esa cabecera `onSendProgress` no se llama nunca (ni con un valor indeterminado) — no es una optimización, es un requisito para que el progreso exista.
3. **El reintento automático por 429 (ya existente desde el slice 1) no es seguro para un cuerpo en streaming, y se corrige explícitamente.** `ApiClient._onError` reintenta releyendo `requestOptions.data`; un `Stream` de `file.openRead()` es de una sola suscripción, así que un reintento real lanzaría `StateError` en vez de un error limpio. Corrección: `_onError` comprueba `requestOptions.data is! Stream` antes de intentar el reintento; si el cuerpo es un stream, cae directo al mapeo de error normal (`rate_limited`), visible en la fila de esa transferencia. El usuario reintenta manualmente, lo que reabre el archivo con un stream fresco. Este problema no existía antes de este slice porque ningún llamador anterior mandaba un cuerpo en streaming.
4. **No se reimplementa borrado de archivo parcial en descargas fallidas**: `dio.download()` ya usa `deleteOnError: true` por defecto. Se confía en ese valor por defecto (con un test que lo verifica) en vez de duplicar la lógica.
5. **Verificación de integridad tras cada descarga.** El propio `FileEntry` (slice 1) ya documentaba que `sha256` se incluía para esto. Tras un `dio.download()` exitoso, se calcula el SHA-256 del archivo ya escrito (paquete `crypto`) y se compara contra la cabecera `X-Content-SHA256` de la respuesta (con `file.sha256` como respaldo si faltara la cabecera); si no coincide, se borra el archivo y se lanza `ApiException(code: 'integrity_mismatch')` en vez de dejar un archivo silenciosamente corrupto. Relevante porque no hay `Range`/reanudación todavía (§41) — un corte a medias sería invisible sin esta comprobación.
6. **Timeout de subida más generoso que el resto de la API.** `receiveTimeout` (30s, global, ADR-009) solo acota la espera de cabeceras/estado, no el envío del cuerpo — para la subida, ese reloj empieza a contar después de mandar el archivo entero y cubre el tiempo que el servidor tarda en escribir+hashear+insertar, proporcional al tamaño. La llamada de subida pasa su propio `Options(receiveTimeout: Duration(minutes: 2))` para no producir un falso "fallo" en el cliente sobre una subida grande que el servidor termina bien.
7. **Misma UX que el cliente web, no una inventada**: subida secuencial (no en paralelo) para selecciones múltiples; progreso y error por transferencia individual (nunca en un banner global); recarga completa del listado tras terminar el lote de subida (nunca inserción optimista); sin estados "disabled" mientras algo está en curso; sin drag-and-drop en este slice (exigiría una dependencia nueva, `desktop_drop`, solo para eso).

## Consecuencias

- Los tests de este slice rompen la convención 100%-en-memoria de los anteriores: `dio.download()` hace I/O real de disco sin ningún punto de inyección, así que sus pruebas usan un directorio temporal real (`Directory.systemTemp`), no solo un adaptador HTTP falso.
- El fix del punto 3 es específico a cuerpos `Stream` — cualquier futura llamada que use un cuerpo distinto (JSON, bytes ya en memoria) sigue beneficiándose del reintento automático por 429 sin cambios.
- Añadir `file_picker` cuando llegue la Fase 4 (Android) es un cambio acotado a la capa de presentación, no un rediseño.
