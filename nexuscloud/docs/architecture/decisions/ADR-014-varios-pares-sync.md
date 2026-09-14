# ADR-014: Varios pares de sincronización -- reentrancia por par y orquestación en un fichero propio

## Estado

Aceptado.

## Contexto

Desde el slice A (ADR-011) el cliente solo admitía **un** par
carpeta-remota/carpeta-local. Slice 13 (dirección) y slice 14 (borrados)
construyeron sobre esa limitación: una dirección global, un manifiesto por
par pero un solo par configurable a la vez. Este slice (15) quita el límite:
el usuario configura una **lista** de pares, cada uno con su propio sentido,
y los sincroniza todos de una vez (a mano o automáticamente).

**Hallazgo real que cambió el diseño, encontrado analizando el código antes
de implementar**: `SyncEngine.syncNow` guardaba su estado de "en curso" en
UN campo (`Future<SyncResult>? _inFlight`), pensado para un único par
posible. Con varios pares, una llamada para el par B mientras el par A
seguía en curso se habría unido **por error** a la Future de A y devuelto
su resultado -- el par B nunca se habría sincronizado de verdad, sin ningún
error visible. Había que arreglar esto antes de poder orquestar varios
pares con seguridad.

**Hallazgo de higiene**: `sync_engine.dart` (886 líneas) y
`sync_settings_page.dart` (651 líneas) ya rozaban o superaban las 800
líneas de golden-principles.md #5 antes de este slice.

## Decisión

1. **Identidad de un par en la lista = `SyncPair.stableKey`** (el hash ya
   introducido en ADR-013). No hace falta un UUID nuevo: dos pares con las
   mismas rutas remota/local ya eran, a todos los efectos, el mismo par
   (mismo manifiesto, misma papelera local) desde ADR-013.

2. **`SyncConfigRepository` pasa de "un par" a "una lista"**: `savePairs`/
   `readPairs` sustituyen a `save`/`read`/`clear`/`saveDirection`/
   `readDirection`. Cada elemento (`SyncPairConfig`) lleva su propia
   `SyncDirection` -- ya no hay una dirección global. Persistencia como un
   único string JSON (`nexuscloud.sync.pairs`) en vez de campos sueltos --
   primera vez que este repositorio necesita (de)serializar una lista, pero
   el patrón "cada entidad se serializa a sí misma" ya lo estableció
   `SyncStateEntry` (ADR-012).
   - **Migración automática y transparente**: si la clave nueva no existe
     pero sí las antiguas (`remote_path`/`local_path`, con `direction`
     opcional -- no existía antes de ADR-012), `readPairs()` construye una
     lista de un elemento, la persiste, borra las claves viejas, y la
     devuelve. Una sesión previa con un par configurado no pierde nada ni
     tiene que reconfigurar. `saveAutoSync`/`readAutoSync`/
     `saveLastAutoSyncOutcome`/`readLastAutoSyncOutcome` **siguen siendo
     globales** -- un único interruptor de auto-sync cubre todos los pares
     (ver "fuera de alcance" más abajo sobre por qué no por par).

3. **Reentrancia de `SyncEngine.syncNow` por par**: `_inFlight` (un campo)
   → `_inFlightByPairKey` (`Map<String, Future<SyncResult>>`, indexado por
   `pair.stableKey`). `isRunning`/`onBusyChanged` pasan a reflejar "hay AL
   MENOS un par en curso" (emite `true` en la primera transición
   vacío→ocupado, `false` en la última ocupado→vacío, no en cada
   entrada/salida individual del mapa). Dos llamadas para el MISMO par
   siguen compartiendo la Future exactamente igual que antes (comportamiento
   sin cambios, con test de regresión); dos llamadas para pares DISTINTOS
   ahora se ejecutan las dos de verdad, sin pisarse.

4. **Orquestación en un fichero propio, no dentro de `SyncEngine`**:
   `MultiPairSyncCoordinator.syncAllNow(pairs, {onStatus,
   confirmedDeletePathsByPair})` recorre la lista **secuencialmente** (mismo
   criterio que el recorrido remoto de ADR-011 dec. 5: nada que paralelizar
   a la escala de §160) y llama a `syncEngine.syncNow` una vez por par. El
   motor sigue siendo, correctamente, el motor de UN par -- "sincronizar un
   par" y "recorrer varios pares" son responsabilidades distintas, y
   añadirlo a `sync_engine.dart` lo habría dejado aún más lejos del límite
   de 800 líneas.
   - **Un fallo de arranque (`ApiException`) en un par no aborta los
     demás**: se atrapa por par y queda como `PairSyncOutcome.startupError`
     de ESE par -- con un solo par, antes de este slice, ese mismo fallo
     simplemente se propagaba a quien llamaba (no había "los demás" que
     proteger).
   - `confirmedDeletePathsByPair` (clave: `pair.stableKey`) enruta cada
     conjunto de confirmaciones (ADR-013) solo a su par -- confirmar un
     lote de borrados de un par nunca afecta a otro.

5. **`AutoSyncScheduler`** delega en el coordinador: un tick lee
   `readPairs()`, sincroniza todos los que haya, y persiste un resumen de
   texto que concatena una línea por par. Nunca rellena
   `confirmedDeletePathsByPair` en ningún par -- ADR-013 se mantiene sin
   cambios: un tick automático jamás confirma un borrado masivo.

6. **UI**: `SyncSettingsPage` pasa de un formulario a una lista de pares
   (añadir/editar vía `PairEditDialog`, quitar, sincronizar todos o uno
   solo) + una tarjeta de resultado por par (`PairSyncResultCard`, extraída
   del bloque que antes vivía inline). Ambos widgets en ficheros propios
   por el mismo motivo de higiene del punto 4.

## Consecuencias

- Los tests de `sync_engine_test.dart` centrados en UN par no necesitaron
  cambios de comportamiento -- solo la reentrancia por par se comprobó con
  un test nuevo dedicado; el resto de la suite (borrados, conflictos,
  direcciones) sigue intacta porque `_runSync`/las tres pasadas no
  cambiaron.
- Editar un par (cambiar sus rutas) no migra ni limpia el manifiesto/la
  papelera local del par "anterior" -- queda huérfano, exactamente igual
  que ya pasaba al reconfigurar el único par que existía antes de este
  slice. No es una regresión, es el mismo comportamiento heredado.
- No hay auto-sync por par (activar/desactivar cada uno por separado): el
  interruptor sigue siendo global. Se descartó por simplicidad -- N
  temporizadores independientes (uno por par) habrían complicado
  `AutoSyncScheduler` sin un caso de uso real todavía; si hace falta, es un
  slice futuro acotado.
- No se detectan pares con rutas solapadas (una carpeta local dentro de
  otra, o la misma carpeta remota en dos pares) -- gap aceptado y
  documentado, igual que la colisión de mayúsculas/minúsculas lo fue en
  ADR-011.
