# ADR-002: Metadatos en base de datos, contenido en filesystem, acceso a través de un único FileService

## Estado

Aceptado.

## Contexto

§9 exige que el contenido de los archivos no viva en la base de datos, y §195-196 exigen que ningún módulo acceda al filesystem sin pasar por una abstracción central. Al mismo tiempo, un explorador de archivos self-hosted es una de las superficies de ataque más peligrosas de todo el proyecto: path traversal, IDOR y symlink attacks son los vectores más comunes contra software de este tipo (§73-74, §198).

Alternativas consideradas para el almacenamiento físico:

- **Object storage desde el día 1 (S3-compatible/MinIO)**: añade una dependencia operativa (otro servicio que instalar/gestionar) injustificada para el caso de uso principal (PC doméstico, Raspberry Pi, §133) y contradice "no sobreingeniería" (§161). Se deja preparado como `Provider` futuro (§157), no como requisito de la Fase 1.
- **Contenido embebido en la base de datos (BLOBs)**: prohibido explícitamente por §9; además degrada el rendimiento de SQLite en instalaciones pequeñas y complica los backups incrementales.

## Decisión

1. Interfaz `storage.Provider` (§102) con una única implementación en Fase 1: `LocalFilesystemProvider`.
2. Un único `FileService` es el punto de entrada obligatorio para cualquier operación sobre archivos de usuario. Valida nombre, normaliza la ruta lógica y comprueba propiedad **antes** de delegar en `Provider`.
3. Toda ruta se resuelve con `storage.SafeJoin`, que anula intentos de path traversal anclando la ruta a una raíz virtual antes de limpiarla (`filepath.Clean("/" + ruta)`), aplicado tanto en `FileService` como, de forma independiente y redundante, dentro del propio `LocalFilesystemProvider` (defensa en profundidad, §168 Zero Trust: ningún componente confía en que otro ya validó).
4. La ruta física en disco incluye el ID del propietario (`<owner_id>/<parent_path>/<name>`), aislando a cada usuario también a nivel de filesystem, no solo de base de datos.
5. Las escrituras son atómicas: fichero temporal en el mismo directorio + `rename` al destino final.

## Consecuencias

- Añadir un segundo `Provider` (S3, NexusCloud remoto) en el futuro no debería requerir cambios en `FileService` ni en los handlers de la API.
- El coste de la defensa en profundidad (`SafeJoin` aplicado dos veces) es marginal en CPU y elimina una clase entera de vulnerabilidades incluso ante un error de programación futuro en una sola de las dos capas.
- Sin sharing todavía (Fase 2), el modelo de propiedad es deliberadamente simple: un archivo pertenece a exactamente un usuario. Introducir compartición requerirá extender la comprobación de autorización en `FileService`, pero no debería requerir cambiar `SafeJoin` ni el diseño de `Provider`.
