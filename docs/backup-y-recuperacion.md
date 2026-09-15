# Backup y recuperación

Guía práctica del Backup Manager. Para el diseño interno y las decisiones
técnicas, ver las ADR-015 a ADR-029 en
[`nexuscloud/docs/architecture/decisions/`](../nexuscloud/docs/architecture/decisions/)
— esta página es solo "cómo lo uso".

## Backup manual, lo básico

```sh
$ nexuscloud --config config.yaml backup run
Backup 68dd6ee6-b4be-410e-a2b5-07cd95ef87ad completado: 1 archivos, 33 B, en /var/lib/nexuscloud/backups/68dd6ee6-b4be-410e-a2b5-07cd95ef87ad

$ nexuscloud --config config.yaml backup list
ID                                    ESTADO     FECHA                ARCHIVOS     TAMAÑO  DESTINO
68dd6ee6-b4be-410e-a2b5-07cd95ef87ad  completed  2026-09-15 14:38:03         1       33 B  /var/lib/nexuscloud/backups
```

Sin `--dest`, respalda en `storage.backupsDir` (por defecto
`<dataDir>/backups`); sin `--pool`, respalda todos los Storage Pools
elegibles (los que no tengan la política `backup: off`).

Cada backup es **todo o nada**: si cualquier archivo falla al respaldarse
(lectura, escritura, o el hash no coincide con lo esperado), el job entero
se marca como fallido y no queda ningún `manifest.json` a medio escribir
— así una restauración nunca puede operar sobre un backup incompleto.

## Verificar la integridad de un backup ya hecho

```sh
$ nexuscloud --config config.yaml backup verify 68dd6ee6-b4be-410e-a2b5-07cd95ef87ad
OK       "default"/Documentos/informe.txt
Backup 68dd6ee6-b4be-410e-a2b5-07cd95ef87ad: 1 archivos verificados correctamente
```

Recalcula el SHA-256 de cada fichero del backup sin restaurar nada — útil
para comprobar periódicamente que el disco de backups no se ha
corrompido, antes de que lo necesites de verdad.

## Restaurar

Dos formas, según lo que necesites:

**A una carpeta cualquiera** (para inspeccionar el contenido, o copiarlo a
mano a donde quieras — no toca la base de datos ni reinserta nada):

```sh
nexuscloud --config config.yaml backup restore 68dd6ee6-b4be-410e-a2b5-07cd95ef87ad --dest /tmp/restaurado
```

**Directamente a un Storage Pool activo**, como archivos navegables/
descargables/versionables de verdad (el escenario real de "algo se
rompió, recupera todo"):

```sh
nexuscloud --config config.yaml backup restore-to-pool 68dd6ee6-b4be-410e-a2b5-07cd95ef87ad --pool default
```

El pool destino es siempre explícito y puede ser distinto del pool
original del backup — precisamente para poder recuperar datos cuando ese
pool original ya no existe o está desactivado. Las carpetas padre que ya
no existieran se recrean automáticamente (un archivo restaurado en
`/Documentos/2024/informe.pdf` es navegable desde la raíz aunque
`Documentos` y `2024` se hubieran borrado).

## Programar backups automáticos

En `config.yaml`:

```yaml
backup:
  enabled: true
  intervalMinutes: 1440       # cada 24h
  retentionCount: 7           # conserva los 7 backups más recientes
  retentionDays: 30           # Y los de los últimos 30 días (unión: cualquiera de los dos motivos basta para conservarlo)
```

Con `enabled: true`, el propio proceso `nexuscloud start`/el servicio
ejecuta backups automáticos en segundo plano según `intervalMinutes` — no
hace falta cron ni una tarea programada aparte. El resto de opciones de
`backup run` (incremental, cifrado, destino remoto) tienen su equivalente
en `config.yaml` para que el backup automático también pueda usarlas:
`incremental: true`, `encrypt: true` (con `NEXUSCLOUD_BACKUP_PASSPHRASE`
puesta en el entorno del servicio) y `remoteDestination: "https://..."`
(con `NEXUSCLOUD_BACKUP_REMOTE_TOKEN`).

## Backup incremental (ahorra espacio)

```sh
nexuscloud --config config.yaml backup run --incremental
```

Los ficheros que no cambiaron desde el backup anterior se enlazan
(hardlink) en vez de recopiarse — mismo contenido, sin duplicar espacio en
disco. Cada backup sigue teniendo su `manifest.json` completo (no es una
cadena de incrementales): se puede restaurar cualquiera de forma aislada,
y borrar uno antiguo (por retención) nunca rompe los demás.

## Backup cifrado

```sh
export NEXUSCLOUD_BACKUP_PASSPHRASE='una-frase-larga-y-real'
nexuscloud --config config.yaml backup run --encrypt
```

AES-256-CTR, clave derivada de la passphrase con Argon2id (los mismos
parámetros ya usados para las contraseñas de usuario). **La passphrase
viaja SOLO por variable de entorno, nunca por flag** — un flag quedaría
visible en el historial de shell y en `ps aux`; esto es así tanto para un
`backup run --encrypt` manual como para el backup automático programado.
No hay ningún prompt interactivo de respaldo: sin la variable, `--encrypt`
falla de inmediato, sin crear ningún job a medias.

Restaurar un backup cifrado necesita la MISMA variable de entorno en el
momento de restaurar:

```sh
NEXUSCLOUD_BACKUP_PASSPHRASE='una-frase-larga-y-real' \
  nexuscloud --config config.yaml backup restore <job-id> --dest /tmp/restaurado
```

Con la passphrase incorrecta, `restore`/`verify` fallan con el mismo tipo
de error que un archivo corrupto — nunca hay corrupción parcial en el
destino ni un falso "restaurado con éxito".

**`--incremental` y `--encrypt` juntos**: siguen funcionando, pero ese run
en concreto no aprovecha la deduplicación por hardlink (cada backup
cifrado usa una clave distinta, así que el contenido cifrado de "el mismo
fichero sin cambios" nunca coincide entre backups aunque el original sea
idéntico). No es un error pedir las dos cosas, solo que ese backup ocupará
el espacio completo.

## Backup a otro servidor NexusCloud (destino remoto)

```sh
export NEXUSCLOUD_BACKUP_REMOTE_TOKEN='el-token-que-configuraste-en-el-receptor'
nexuscloud --config config.yaml backup run --dest https://otro-servidor.ejemplo:8080
```

El servidor receptor necesita arrancar con la variable de entorno
`NEXUSCLOUD_BACKUP_RECEIVE_TOKEN` puesta al mismo valor (vacía por
defecto — secure by default: si no la configuras, esa instancia ni
siquiera registra las rutas HTTP de recepción). Igual que la passphrase de
cifrado, el token viaja solo por variable de entorno, nunca por flag ni en
`config.yaml`.

## Mantener solo los backups que necesitas

```sh
nexuscloud --config config.yaml backup run --keep-last 7        # solo los 7 más recientes
nexuscloud --config config.yaml backup run --keep-days 30       # solo los de los últimos 30 días
```

Se aplican DESPUÉS de completar el run actual, sobre los backups en ese
mismo `--dest` — combinables con `--incremental`/`--encrypt`.

## Antes de necesitarlo de verdad

- Prueba `backup restore`/`restore-to-pool` al menos una vez en frío,
  antes de que lo necesites de verdad — la primera vez que restauras algo
  no debería ser en medio de una emergencia.
- Si el destino de backup es un disco distinto al de los datos (lo
  recomendable), verifícalo con `nexuscloud storage disks`.
- `backup verify` periódico detecta bitrot en el disco de backups antes de
  que sea demasiado tarde.
