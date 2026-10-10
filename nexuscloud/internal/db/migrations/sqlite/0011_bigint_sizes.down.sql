-- SQLite no tenía columnas de 32 bits: solo se retira el índice de uso.
DROP INDEX idx_files_owner_usage;
