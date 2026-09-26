-- Ver migrations/sqlite/0014_favorites.up.sql -- mismo razonamiento. Mismas
-- divergencias que 0005_sharing en este dialecto: el CHECK se aplica de
-- verdad (MySQL 8.0.16+/MariaDB 10.2.1+, ya exigido por el proyecto);
-- user_id/file_id/directory_id no llevan índice propio explícito -- InnoDB
-- ya crea uno automático para cada FK de una sola columna --, solo se
-- declaran los dos UNIQUE compuestos, que sí hacen falta.
CREATE TABLE favorites (
    id           VARCHAR(36) PRIMARY KEY,
    user_id      VARCHAR(36) NOT NULL,
    file_id      VARCHAR(36),
    directory_id VARCHAR(36),
    created_at   TEXT NOT NULL,
    CONSTRAINT fk_favorites_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_favorites_file_id FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    CONSTRAINT fk_favorites_directory_id FOREIGN KEY (directory_id) REFERENCES directories(id) ON DELETE CASCADE,
    CONSTRAINT chk_favorites_file_xor_directory CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL)),
    UNIQUE (user_id, file_id),
    UNIQUE (user_id, directory_id)
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
