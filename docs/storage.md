# Motor de almacenamiento

## Principio (§9)

La base de datos solo contiene metadatos (ruta, tamaño, hash, propietario, tipo MIME, timestamps). El contenido de los archivos vive siempre en el filesystem, nunca en la base de datos.

## Capas

```
API handlers (internal/api/v1)
        │  valida sesión + pertenencia
        ▼
FileService (internal/storage)     ← único punto de acceso a archivos de usuario
        │  valida nombre/ruta, resuelve pool
        ▼
Provider interface                  ← abstracción (§102)
        │
        ▼
LocalFilesystemProvider             ← única implementación en Fase 1
        │  SafeJoin + escritura atómica
        ▼
Filesystem real
```

Ningún otro paquete debe tocar `Provider` directamente ni construir rutas de archivo a mano (§195-196): todo pasa por `FileService`.

## Storage Pools (§10)

`storage_pools` modela pools con `name`, `type` (`local` en Fase 1), `path`, `priority`, `status`. Al arrancar, `storage.EnsureDefaultPool` crea un pool `default` si no existe ninguno — el repositorio en sí nunca crea pools implícitamente, para que la lógica de arranque sea explícita y auditable.

RAID (§11-12) y detección de discos/SMART quedan para la Fase 5: NexusCloud nunca implementará su propio RAID — detectará y mostrará el estado de las tecnologías que ya ofrezca el sistema operativo (mdadm, Storage Spaces, ZFS/Btrfs).

## Identificadores

Los IDs de archivo son UUID v4 aleatorios (`internal/idgen`), nunca incrementales — evita enumeración e IDOR (§123, §198-199). La ruta física en disco (`<owner_id>/<parent_path>/<name>`) aísla a cada propietario incluso a nivel de filesystem.

## Integridad

Cada subida calcula su SHA-256 mientras escribe (un único `io.Copy` con `io.MultiWriter` hacia el fichero y el hasher, sin cargar el contenido completo en memoria, §136-137). La escritura es atómica: fichero temporal + `rename`, así que un fallo a mitad de subida nunca downgradea un archivo existente a un estado corrupto.

## Qué falta (fases posteriores)

- **Versionado/Papelera** (§15-16): en Fase 1, subir al mismo `parent_path`+`name` sobrescribe el archivo conservando su ID (upsert); no hay historial de versiones ni papelera todavía — esas tienen sus propias tablas reservadas para la Fase 2.
- **Snapshots** (§17): delegado al filesystem/SO subyacente (ZFS/Btrfs/Storage Spaces) cuando llegue.
- **Backups** (§18): Backup Manager independiente, Fase 5.
- **Compartición y enlaces públicos** (§37): Fase 2.
- **Miniaturas/previsualización/búsqueda de contenido** (§33-35): Fase 2.
- **Range requests / descargas reanudables** (§41): el endpoint de descarga transmite el contenido completo; soporte de `Range` queda pendiente sin que suponga un cambio de contrato de API cuando se añada.
