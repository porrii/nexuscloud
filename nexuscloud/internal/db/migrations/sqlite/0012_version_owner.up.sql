-- Propietario de cada versión, copiado de files.owner_id (ADR-036). El uso de
-- versiones de un usuario se calculaba con un JOIN entre file_versions y TODOS sus
-- archivos: con 200 000 archivos y 40 000 versiones costaba de 46 a 77 ms en
-- PostgreSQL, de 260 a 340 en SQLite y de 175 a 220 en MySQL, y era lo que más pesaba
-- de cada comprobación de cuota. Con el propietario en la propia tabla el uso es una
-- sola lectura del índice (owner_id, size_bytes), sin tocar files: 4, 10 y 12 ms.
--
-- Es una desnormalización segura porque un archivo nunca cambia de propietario
-- (la clave natural lo incluye y ninguna operación lo transfiere); el repositorio
-- lo toma de la propia fila de files al crear la versión, y la columna es NOT NULL
-- para que un olvido falle en voz alta en vez de contarse de menos en silencio.
--
-- SQLite no permite añadir una columna NOT NULL sin valor por defecto a una tabla
-- con filas, así que se reconstruye la tabla (el procedimiento que recomienda la
-- propia documentación de SQLite): se crea la nueva con la columna, se copian las
-- filas con su propietario, se sustituye y se recrean los dos índices.
CREATE TABLE file_versions_new (
    id           TEXT PRIMARY KEY,
    file_id      TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    owner_id     TEXT NOT NULL,
    version_num  INTEGER NOT NULL,
    size_bytes   INTEGER NOT NULL,
    sha256       TEXT NOT NULL,
    mime_type    TEXT NOT NULL,
    storage_key  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    UNIQUE (file_id, version_num)
);
INSERT INTO file_versions_new (id, file_id, owner_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at)
    SELECT v.id, v.file_id, f.owner_id, v.version_num, v.size_bytes, v.sha256, v.mime_type, v.storage_key, v.created_at
    FROM file_versions v JOIN files f ON f.id = v.file_id;
DROP TABLE file_versions;
ALTER TABLE file_versions_new RENAME TO file_versions;
CREATE INDEX idx_file_versions_file_id ON file_versions(file_id, version_num DESC);
CREATE INDEX idx_file_versions_owner_usage ON file_versions(owner_id, size_bytes);
