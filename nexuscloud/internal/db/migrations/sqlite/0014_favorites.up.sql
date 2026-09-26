-- Favoritos (§87, ADR-038): un archivo o una carpeta que un usuario marca
-- para encontrarla rápido. Mismo patrón exacto que shares (migración 0005):
-- file_id/directory_id son columnas de recurso separadas (no un par
-- resource_type/resource_id sin FK real) para que ON DELETE CASCADE limpie
-- favoritos huérfanos automáticamente al borrar un archivo o carpeta para
-- siempre. El CHECK exige que se rellene exactamente una de las dos.
--
-- Solo sobre el árbol PROPIO del usuario (decisión de producto, ADR-038):
-- no hay ninguna columna para "compartido conmigo" -- FileService.AddFavorite
-- comprueba la propiedad del recurso antes de insertar, en Go, igual que
-- validateName valida en Go y no aquí.
--
-- Los dos UNIQUE compuestos evitan duplicar el mismo favorito; un usuario
-- puede tener como mucho una fila con un file_id dado y una fila con un
-- directory_id dado (varias filas con la otra columna en NULL no chocan:
-- NULL ≠ NULL en un UNIQUE, en los tres motores).
CREATE TABLE favorites (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id      TEXT REFERENCES files(id) ON DELETE CASCADE,
    directory_id TEXT REFERENCES directories(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL)),
    UNIQUE (user_id, file_id),
    UNIQUE (user_id, directory_id)
);
CREATE INDEX idx_favorites_user ON favorites(user_id);
CREATE INDEX idx_favorites_file ON favorites(file_id) WHERE file_id IS NOT NULL;
CREATE INDEX idx_favorites_directory ON favorites(directory_id) WHERE directory_id IS NOT NULL;
