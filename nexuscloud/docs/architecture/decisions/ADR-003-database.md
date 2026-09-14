# ADR-003: Abstracción multi-motor sin ORM, SQLite por defecto + PostgreSQL

## Estado

Aceptado.

## Contexto

§8 exige soportar como mínimo SQLite (instalaciones pequeñas) y PostgreSQL (instalaciones mayores), seleccionable por configuración, sin que cambiar de motor implique tocar el código de NexusCloud. MariaDB/MySQL se marca como deseable pero no mínimo obligatorio.

Alternativas consideradas:

- **ORM completo (GORM, ent)**: simplifica el CRUD básico pero oculta el SQL real, dificultando auditar exactamente qué consulta se ejecuta — en un proyecto donde SQL injection es una amenaza explícitamente listada (§72), preferimos SQL visible y parametrizado a mano sobre "magia" generada.
- **sqlc (SQL a código Go)**: genera código type-safe a partir de SQL real, pero su generación por dialecto complica mantener una única fuente de consultas portable entre sqlite/postgres sin duplicar todo el código de acceso a datos.
- **Driver SQLite basado en CGO** (`mattn/go-sqlite3`): es el más usado del ecosistema, pero requiere un toolchain de compilación C para cada plataforma objetivo (incluida compilación cruzada a ARM para Raspberry Pi y a Windows), lo que complica significativamente CI/CD y la distribución de binarios estáticos.

## Decisión

1. `database/sql` de la librería estándar + interfaces `Repository` propias por dominio (`users.Repository`, `auth.SessionRepository`, `storage.FileRepository`, etc.), implementadas con SQL escrito a mano.
2. Driver SQLite: **`modernc.org/sqlite`**, una reimplementación en Go puro (sin CGO) — permite `CGO_ENABLED=0` y compilación cruzada trivial a ARM64/Windows, crítico para Raspberry Pi (§64) e instalación sencilla (§60).
3. Driver PostgreSQL: **`jackc/pgx/v5`**, el estándar de facto moderno, también sin CGO.
4. Un pequeño helper (`db.Conn.Rebind`) reescribe los placeholders `?` al estilo `$1, $2, ...` que exige PostgreSQL, permitiendo escribir cada consulta una única vez y mantenerla portable, sin necesidad de un ORM completo.
5. Todas las columnas de timestamp se almacenan como `TEXT` en formato RFC3339Nano UTC en **ambos** dialectos, deliberadamente, para evitar diferencias de comportamiento entre drivers al escanear tipos nativos de fecha/hora (`time.Time` vs `string` según el driver y la configuración).
6. Migraciones con `golang-migrate/migrate`, ficheros `.up.sql`/`.down.sql` explícitos por dialecto embebidos en el binario (`go:embed`), nunca destructivas por defecto (§8).
7. MySQL/MariaDB queda fuera de esta pasada. ~~Las interfaces ya están desacopladas del dialecto, así que añadir un tercer driver + directorio de migraciones no debería requerir cambios en la lógica de negocio.~~ **Corrección ([ADR-031](ADR-031-mysql-mariadb.md), verificado empíricamente contra `mysql:8`/`mariadb:11` reales al implementarlo): esta afirmación era incorrecta.** MySQL/MariaDB exigen `VARCHAR` explícito en toda columna PK/FK/indexada/con `DEFAULT` (rechazan `TEXT` ahí), ignoran en silencio la sintaxis de FK inline que usan las migraciones de sqlite/postgres, y no soportan la sintaxis de `UPSERT`/concatenación de cadenas (`ON CONFLICT`, `||`) que ya usa el código de `internal/storage` -- sí hicieron falta cambios reales en 3 repositorios de dominio, no solo un driver y unas migraciones nuevas.

## Consecuencias

- SQLite se abre con una única conexión (`SetMaxOpenConns(1)`) para evitar errores de bloqueo bajo escritura concurrente con el driver puro Go — se prioriza corrección sobre concurrencia de lectura, razonable a la escala objetivo de ~100 usuarios (§133, §160). PostgreSQL no tiene esta restricción.
- El esquema deliberadamente "aburrido" (todo `TEXT`, sin tipos nativos avanzados como `JSONB`) es más portable pero renuncia a algunas optimizaciones específicas de PostgreSQL (búsqueda en JSON, índices GIN) que podrían añadirse más adelante sin romper compatibilidad con SQLite si se aíslan detrás de la interfaz `Repository`.
- Auditar cualquier consulta es tan simple como leer el fichero `.go` correspondiente: no hay generación de código ni SQL oculto detrás de un ORM.
