# ADR-031: Soporte MySQL/MariaDB como tercer dialecto de base de datos (§8)

## Estado

Aceptado.

## Contexto

§8 marca MySQL/MariaDB como "deseable pero no mínimo obligatorio" (ADR-003)
-- SQLite y PostgreSQL ya cumplían el mínimo. ADR-003 punto 7 afirmaba que
añadir un tercer dialecto "no debería requerir cambios en la lógica de
negocio" porque las interfaces ya estaban desacopladas del dialecto.

**Esa afirmación resultó incorrecta**, verificado empíricamente contra
contenedores reales `mysql:8` y `mariadb:11` (Docker) antes de escribir
ninguna migración o rama de código, no solo leyendo documentación:

1. **`TEXT` no puede usarse en ninguna columna que sea `PRIMARY KEY`,
   participe en un `UNIQUE`/`INDEX`, o sea columna de `FOREIGN KEY`** --
   MySQL exige longitud de clave explícita (`ERROR 1170`). Casi todas las
   tablas del esquema usan `id TEXT PRIMARY KEY`: no es una excepción
   puntual, es la mayoría del esquema.
2. **La sintaxis `columna TIPO REFERENCES tabla(col) ON DELETE accion` (FK
   inline, la que usan las migraciones de sqlite/postgres) se ignora EN
   SILENCIO en MySQL 8** -- confirmado con `SHOW CREATE TABLE` (no aparece
   ninguna constraint) y con un `INSERT` con FK inexistente aceptado sin
   error. **En MariaDB 11 sí se honra** -- si solo se hubiera probado
   contra MariaDB, este problema no se habría detectado; como un único
   driver (`go-sql-driver/mysql`) cubre ambos motores, la FK se escribe
   siempre como cláusula explícita.
3. **MySQL/MariaDB prohíben `DEFAULT 'literal'` sobre una columna `TEXT`**
   (`ERROR 1101`) -- afecta a 9 columnas hoy `TEXT` con `DEFAULT`
   (`users.status`, `storage_pools.type`/`status` + las 4 políticas de
   `0006`, `files.mime_type`, `shares.label`).
4. **Ni MySQL 8 ni MariaDB 11 son sensibles a mayúsculas/acentos por
   defecto** (`utf8mb4_0900_ai_ci` / `utf8mb4_uca1400_ai_ci`,
   respectivamente) -- a diferencia de sqlite/postgres, que sí lo son sin
   configuración adicional. Sin `COLLATE utf8mb4_bin` explícito,
   "Documentos" y "documentos" colisionarían como nombre duplicado solo en
   MySQL/MariaDB.
5. **La sintaxis de `UPSERT` (`ON CONFLICT`, 2 sitios) no existe en
   MySQL** -- necesita `INSERT IGNORE` / `INSERT ... ON DUPLICATE KEY
   UPDATE`.
6. **La concatenación `? || substr(...)` (2 sitios, `MoveDirectoryTree`)
   es el operador lógico OR en MySQL por defecto** -- síntoma en tiempo de
   EJECUCIÓN, no de compilación. Además, la cláusula `ESCAPE '\'`
   compartida en esas mismas sentencias necesita `ESCAPE '\\'` (doble
   backslash) en MySQL/MariaDB, que sí tratan `\` como carácter de escape
   dentro de un literal `'...'` (sqlite/postgres, con
   `standard_conforming_strings`, no).

## Decisión

1. **Driver**: `github.com/go-sql-driver/mysql` -- Go puro (sin CGO, mismo
   criterio anti-CGO que sqlite/postgres, ADR-003), estándar de facto, y
   cubre MariaDB con el mismo driver por compatibilidad de protocolo de
   red (no hace falta un driver separado por motor).
2. **Mínimo de versión soportado: MySQL 8.0.16+ / MariaDB 10.2.1+** -- los
   primeros que *aplican* el `CHECK` de `shares` (hallazgo 4 de ADR-024 /
   §37), no solo lo parsean.
3. **`internal/db/db.go`**: tercer `case "mysql"` en `Open()` (nuevo
   `openMySQL(dsn)`, que parsea el DSN con `mysqldriver.ParseDSN` y fuerza
   `MultiStatements = true` antes de `FormatDSN()` -- lo necesita
   `golang-migrate` para aplicar ficheros con varias sentencias; ningún
   repositorio de dominio concatena nunca más de una sentencia por
   `ExecContext`, así que esto no abre ninguna superficie nueva a datos de
   usuario) y en `newMigrator()` (vía
   `github.com/golang-migrate/migrate/v4/database/mysql`, ya incluido en
   la versión de `golang-migrate` ya usada -- sin librería nueva más allá
   del driver). Import con alias (`migratemysql`/`mysqldriver`): el driver
   y el paquete de migración se llaman ambos `mysql`.
4. **`internal/db/conn.go` (`Rebind`) no cambia**: ya trata "cualquier
   driver que no sea postgres" como passthrough, y `go-sql-driver/mysql`
   acepta `?` nativo igual que sqlite -- mysql ya caía en la rama correcta
   sin ningún cambio.
5. **Migraciones** (`internal/db/migrations/mysql/`, 7 pares
   `.up`/`.down.sql`): mismo esquema que sqlite/postgres, con estas reglas
   aplicadas de forma sistemática:
   - Toda columna `TEXT` que sea PK, participe en un `UNIQUE`/`INDEX`, sea
     columna de FK, o tenga `DEFAULT`, pasa a `VARCHAR(n)`. Tamaños:
     `VARCHAR(36)` uniforme para todo id/columna-de-FK-a-id (longitud
     exacta de un UUID de `idgen.New()`), `VARCHAR(64)` para `token_hash`
     (longitud exacta de `hex(sha256(...))`), `VARCHAR(32)` para
     timestamps indexados (RFC3339Nano) y enums cortos con `DEFAULT`,
     `VARCHAR(255)` para nombres únicos de administrador.
   - **`parent_path`/`name` de `files`/`directories`: `VARCHAR(400)`/
     `VARCHAR(255)`** -- calculado para que el índice compuesto
     `UNIQUE(pool_id, owner_id, parent_path, name)` (2×36+400+255 = 727
     caracteres × 4 bytes utf8mb4 en el peor caso = 2908 bytes) quepa en
     el límite de InnoDB de 3072 bytes por índice (verificado: con
     `parent_path VARCHAR(1024)` falla con `ERROR 1071: Specified key was
     too long`). **Límite nuevo que no existe en sqlite/postgres hoy** (no
     hay ninguna validación de longitud de nombre/ruta en el código Go
     actual) -- 255/400 caracteres son generosos para el uso real, y
     documentarlo aquí es preferible a añadir validación Go nueva fuera
     del alcance de este trabajo.
   - Toda FK inline se escribe como `CONSTRAINT fk_<tabla>_<columna>
     FOREIGN KEY (col) REFERENCES tabla(col) ON DELETE accion` explícita.
   - Todo `CREATE TABLE` termina en `ENGINE=InnoDB CHARACTER SET utf8mb4
     COLLATE utf8mb4_bin`.
   - Cuando un índice de sqlite/postgres coincide 1:1 con el índice
     automático que InnoDB crea para una FK de una sola columna, se omite
     (evita duplicar índice sin beneficio).
   - Los 4 índices parciales de `0005_sharing` (`WHERE ... IS NOT NULL`)
     se convierten en índices completos: MySQL/MariaDB no soportan índices
     parciales. La unicidad de `token_hash` con múltiples `NULL` se
     preserva igual (los tres motores permiten varios `NULL` en un índice
     `UNIQUE`) -- solo se pierde la optimización de tamaño del índice. El
     `CHECK` de esa misma migración se mantiene tal cual (el suelo de
     versión de arriba ya garantiza que se aplica de verdad).
6. **Ramas por dialecto, inline** (no un helper/query-builder compartido):
   `CreateDirectory` (`internal/storage/directory_sql_repository.go`,
   `INSERT IGNORE` -- equivalente en la práctica al `ON CONFLICT` actual
   porque `directories` solo tiene 2 restricciones; el único caso en que
   difiere es una colisión de `id` UUID, criptográficamente despreciable,
   que postgres seguiría rechazando y MySQL ignoraría en silencio, riesgo
   aceptado), `UpsertFile` (`internal/storage/file_sql_repository.go`,
   `ON DUPLICATE KEY UPDATE ... VALUES(col)` -- nunca el alias `AS new`,
   que MariaDB nunca implementó), y `MoveDirectoryTree`
   (`internal/storage/directory_sql_repository.go`, `CONCAT`/
   `ESCAPE '\\'` en las 2 sentencias `UPDATE`). Son solo 3 sitios, cada
   variante es una sentencia SQL completa y autocontenida -- un helper
   genérico habría empujado hacia el mini-ORM que ADR-003 punto 1 ya
   rechazó a propósito.
7. **`isUniqueViolation`** (duplicada a propósito en
   `internal/storage/sqlerr.go` y `internal/users/sql_repository.go`,
   mismo criterio de siempre): añade `strings.Contains(msg, "duplicate
   entry")` -- el texto real de MySQL/MariaDB para una violación `UNIQUE`
   (`"Duplicate entry 'x' for key 'y'"`) no contenía ni "unique" ni la
   subcadena contigua "duplicate key", así que sin este cambio el caller
   habría recibido un error 500 genérico en vez de `ErrAlreadyExists`.
   `isForeignKeyViolation` (`internal/storage/pool_sql_repository.go`) NO
   cambió -- verificado que el mensaje real de MySQL/MariaDB ya contiene
   "foreign key", cerrado con un test de integración real en vez de
   dejarlo como suposición.
8. **Testing real contra Docker** (`internal/dbtest`, arnés nuevo -- no
   existía ningún patrón previo de tests Go levantando contenedores en
   este repo): `dbtest.OpenReal(t, driver)` lee
   `NEXUSCLOUD_TEST_<DRIVER>_DSN`, hace `t.Skip` si no está puesta (el
   resto de `go test ./...` no depende de Docker), y si está puesta abre
   la conexión y aplica migraciones por el camino REAL de producción
   (`db.Open`+`db.Migrate`). Parametrizado por `driver`, así que el mismo
   arnés sirve también para `"postgres"` sin código adicional -- cierra
   como beneficio colateral el hueco, documentado en ADR-006, de que
   Postgres nunca se había verificado contra una instancia real en este
   repo (ni en esta sesión de desarrollo, ni en CI). `scripts/dev.sh` gana
   subcomandos `test-mysql`/`test-mariadb`/`test-postgres` que levantan el
   contenedor correspondiente, esperan una conexión autenticada real
   (nunca un `sleep` fijo ni solo "puerto abierto" -- ambos dan falso
   positivo mientras la contraseña root todavía se está aplicando), corren
   `go test ./...` con el DSN real, y limpian los recursos Docker al
   terminar (éxito o fallo). No están en el subcomando `all` por defecto
   (más lentos, necesitan Docker) -- se invocan explícitamente.

## Fuera de alcance, documentado pero no arreglado aquí

`size_bytes`/campos similares son `INT` (32 bits) también en el PostgreSQL
real de HOY, no solo en la migración mysql nueva -- un fichero de más de
~2GiB ya desbordaría contra un Postgres real (sqlite no sufre esto porque
su `INTEGER` no se limita de verdad a 32 bits). Es una inconsistencia
sqlite-vs-postgres **preexistente**, no introducida por este trabajo; se
mantiene el mismo `INT` en mysql por consistencia con lo ya decidido.
Subir los tres dialectos a 64 bits sería una migración `0008` aparte que
el usuario debería pedir explícitamente, no algo que cuele aquí sin que se
note.

## Consecuencias

- MySQL/MariaDB queda al mismo nivel de soporte que sqlite/postgres:
  seleccionable por `database.driver: mysql` sin tocar código, migraciones
  automáticas, mismo comportamiento observable en los 3 repositorios que
  necesitaron rama por dialecto.
- ADR-003 punto 7 queda corregido con un enlace cruzado a este documento.
- `docs/storage.md` documenta el límite nuevo de `parent_path`/`name` y
  las divergencias de SQL reales.
- El arnés de test contra Docker (`internal/dbtest`) es reutilizable para
  cualquier verificación futura contra un motor real, no solo para este
  trabajo -- ya cierra también el hueco de Postgres nunca verificado.
- Verificado con `scripts/dev.sh all` (sqlite, camino existente sin
  cambios de comportamiento) y `scripts/dev.sh test-mysql`/`test-mariadb`/
  `test-postgres` (los 3 motores reales) -- ver detalle de qué se ejercitó
  en cada uno en `internal/db/mysql_integration_test.go`,
  `internal/storage/mysql_integration_test.go` e
  `internal/users/mysql_integration_test.go`.

## Referencias

- [ADR-003: Abstracción multi-motor sin ORM](ADR-003-database.md) -- la
  decisión base que este ADR corrige y extiende.
- [ADR-006: Papelera](ADR-006-trash.md) -- documentaba el hueco de
  Postgres nunca verificado, cerrado como beneficio colateral por el
  arnés de test de este ADR.
