DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS storage_pools;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS user_groups;
-- "groups" (comillas invertidas): palabra reservada en MySQL 8+ (unidad de
-- ventana ROWS|RANGE|GROUPS, SQL:2016) -- ver la nota en 0001_initial.up.sql.
DROP TABLE IF EXISTS `groups`;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
