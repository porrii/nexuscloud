# ADR-017: Retención de backups — mantener los últimos N y/o los de los últimos N días por destino, poda best-effort

## Estado

Aceptado.

## Contexto

El Slice 2 (ADR-016) activó el backup automático programado, pero sin ningún límite: cada tick añade un backup completo más a `cfg.BackupsDir()`, sin borrar nunca los anteriores. El propio ADR-016 lo dejó anotado como el hueco más urgente que quedaba abierto.

§18 exige "Retención". §173 exige explícitamente **"no eliminar automáticamente backups sanos simplemente porque exista uno nuevo"**. Estas dos exigencias parecen tensionadas a primera vista -- toda retención automática, por definición, borra backups antiguos como consecuencia de que uno nuevo tuvo éxito.

## Decisión

**Lectura de §173 aplicada**: la advertencia no prohíbe la rotación en sí (si lo hiciera, "Retención" de §18 sería imposible de cumplir) — prohíbe el patrón peligroso de "borrar el/los backup(s) anteriores por el mero hecho de que uno nuevo tuvo éxito, sin ninguna red de seguridad". La red de seguridad real es doble, y ya la daba el propio diseño de ADR-015 más una regla nueva de este slice:

1. **Un backup nunca se marca `completed` hasta que TODOS sus ficheros están verificados** (`Run` es todo-o-nada, ADR-015) — nunca hay un "éxito" falso que dispare una rotación basada en un backup en realidad corrupto o a medias.
2. **La política es "mantener los últimos N", nunca "mantener solo el más reciente" ni "borrar todo lo antiguo sin más"** — `RunOptions.RetentionCount`, con `N >= 1` configurado, garantiza por construcción que SIEMPRE queda al menos un backup sano tras podar; `N = 0` (por defecto) significa "sin límite", nunca borra nada.
3. **La retención es por destino (`DestinationPath`), no global.** Un backup manual a un USB externo y los backups automáticos diarios al disco local son conteos independientes -- podar unos nunca afecta a los otros, evitando que el scheduler automático borre sin querer un backup manual que el administrador quería conservar aparte.
4. **La poda es *best-effort* y deliberadamente silenciosa ante fallos individuales.** El backup que se acaba de completar YA tuvo éxito y ya está verificado en disco antes de que se intente podar nada; si borrar una carpeta antigua falla (permisos, disco de solo lectura, etc.), eso nunca debe reportarse como si el backup NUEVO hubiera fallado -- serían dos problemas de gravedad muy distinta mezclados en un único error. Un backup antiguo huérfano en disco es un problema mucho menor que ocultar que el nuevo backup sí se hizo bien. Por la misma razón, la fila de `backup_jobs` de un job podado solo se borra SI su carpeta en disco se pudo borrar antes -- nunca se deja una fila en la base de datos apuntando a una carpeta que ya no existe, ni una carpeta huérfana sin ninguna fila que la explique salvo que el propio borrado de la carpeta fallara primero.
5. **Los jobs `failed` nunca se cuentan ni se podan.** No son "backups sanos" que rotar -- son ruido de depuración de un intento que no llegó a buen puerto; se excluyen del todo del cálculo de "los N más recientes".
6. **Retención por cantidad (`RetentionCount`) Y por antigüedad (`RetentionDays`), compuestas como "unión de motivos para conservar".** Un backup se poda solo si TODAS las políticas activas (`> 0`) votan podarlo; una política en `0` no vota ni a favor ni en contra, simplemente no participa. Con las dos activas, basta con que UNA quiera conservarlo para que sobreviva -- nunca al revés. Ejemplos reales que esto cubre: `RetentionCount=2` puro podaría un backup de hace 2 horas si ya hay 2 más recientes, pero con `RetentionDays=30` también activo, ese backup de 2 horas sobrevive (la edad lo salva de una cuenta agresiva); simétricamente, `RetentionDays=30` puro podaría los dos únicos backups que existen si tienen 45 días, pero con `RetentionCount=5` también activo, ambos sobreviven (la cuenta los salva porque no hay ni 5 backups en total). Esta composición evita que activar la segunda política vuelva la retención MÁS agresiva de lo que el administrador esperaba al configurar solo una.

`RunOptions.RetentionCount`/`RetentionDays` (ambos `0` = sin límite) son la única API: tanto `nexuscloud backup run --keep-last N --keep-days N` (manual) como el scheduler automático (`cfg.Backup.RetentionCount`/`RetentionDays`) pasan por el mismo camino en `Manager.Run`, sin duplicar la lógica de poda en dos sitios.

## Consecuencias

- Un administrador que active `backup.retentionCount` en `config.yaml` junto con el backup automático obtiene, por fin, un espacio en disco acotado y predecible -- cierra el riesgo que ADR-016 dejó anotado explícitamente.
- Sin retención (`0`, el valor por defecto), el comportamiento es exactamente el mismo que en el Slice 1/2: nunca se borra nada solo, coherente con "seguro por defecto" en toda esta área.
- Un fallo de poda silencioso significa que, en el peor caso, un disco puede acumular más backups de los N configurados si el borrado de carpetas empieza a fallar sistemáticamente (p.ej. permisos cambiados a mano) -- se acepta este riesgo porque la alternativa (fallar el `Run` recién completado con éxito) sería peor: mezclaría un problema de limpieza con un backup que en realidad sí se hizo bien.
- Añadir en el futuro una notificación/log cuando la poda falla repetidamente sería una extensión de bajo riesgo sobre este mismo diseño, no un cambio de arquitectura.
