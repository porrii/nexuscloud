# ADR-009: Cimientos del cliente Flutter — corrige ADR-005 en dos puntos, añade elección de HTTP y ubicación del token

## Estado

Aceptado.

## Contexto

Fase 3 (§163) exige un cliente Desktop Windows/Linux en Flutter/Dart. [ADR-005](ADR-005-multiplatform-strategy.md) y `client/README.md` ya establecían que el cliente reutilizaría el patrón validado en NexusKeys: Clean Architecture por feature, `get_it` para inyección de dependencias, `flutter_riverpod` para estado, `flutter_secure_storage`/`local_auth` para credenciales y biometría.

Al empezar a implementar el primer slice (login + explorador de archivos de solo lectura) se inspeccionó directamente el código de NexusKeys — no solo su README — y dos afirmaciones de ADR-005 resultaron ser aspiracionales, no reales:

1. **`flutter_riverpod` está en `pubspec.yaml` pero no se usa en ningún archivo** de NexusKeys (cero coincidencias de `riverpod`, `StateNotifier`, `ConsumerWidget`, `@riverpod` en `lib/` ni `test/`). El manejo de estado real es: repositorios de dominio con un getter síncrono (`List<VaultItem> get currentItems`) más un `Stream` broadcast no-replay (`Stream<List<VaultItem>> get itemsStream`), consumidos desde `StatefulWidget` vía `setState`/`StreamBuilder`, con dependencias resueltas de `get_it`.
2. **`local_auth` no tiene implementación real en Linux** (solo `local_auth_windows`/`local_auth_android`/`local_auth_darwin` existen como plugins federados) — irrelevante para este slice (biometría no es uno de los 8 puntos de §45), pero afecta cualquier fase futura que la asuma disponible en los tres SO.

Además, este slice introduce dos necesidades que NexusKeys nunca tuvo al ser 100% offline: hablar con una API REST remota (cliente HTTP) y guardar un token de sesión que **más de una feature** necesita leer (no solo `auth`).

## Decisión

1. **No usar `flutter_riverpod` en este slice.** Se sigue la práctica *real* de NexusKeys (getter síncrono + `Stream` no-replay + `StatefulWidget`/`setState`/`StreamBuilder`, resuelto vía `get_it`), no la aspiracional. Motivo: cada ADR existente de este proyecto (ADR-004 rechazó JWT pese a ser más estándar; ADR-006 rechazó el índice único parcial "correcto" por no poder verificarse en esa sesión) elige la opción más simple ya probada sobre la "técnicamente mejor" salvo que exista una razón técnica concreta (§162). Las ~3 llamadas de red de este slice (login, `/users/me`, listar un directorio) se representan correctamente con `Stream`+`setState` — el propio cliente web ya lo demuestra sin librería de estado alguna (`web/src/pages/FilesPage.tsx`). Si un slice futuro (motor de sync) demuestra dolor real de `setState` con estado async genuinamente más complejo (colas de subida, listas de conflictos, estado en vivo), se reconsidera entonces con evidencia concreta.
2. **`local_auth` queda fuera de este slice** (no es requisito de §45); si una fase posterior lo necesita en Linux, se evalúa entonces con `os/exec`-style capability detection (§101) en vez de asumir soporte universal.
3. **`dio` como cliente HTTP, no `package:http`.** El cliente web (`web/src/api/client.ts`) ya tuvo que bajar a `XMLHttpRequest` crudo porque `fetch`/`http` no exponen progreso de subida — comentario explícito en el código: *"Sube contenido con progreso real (fetch no lo expone en subida)"*. `dio` da `onSendProgress`/`onReceiveProgress` de fábrica, necesarios en el slice de subida/descarga que sigue a este. Su cadena de interceptores centraliza en un solo sitio: adjuntar `Authorization: Bearer`, traducir el sobre de error `{"error":{"code","message"}}` en una excepción tipada, detectar 401 de sesión caducada y reintentar 429 con backoff — evita repetir esa lógica en cada método de cada repositorio. Su `HttpClientAdapter` es sustituible por un fake hand-written en tests, sin necesitar `mocktail`/`mockito` (que NexusKeys tampoco usa).
4. **`TokenStore` y `ServerConfigStore` viven en `core/storage/`, no en `features/auth/`**, aunque conceptualmente son "de auth". NexusKeys ya resolvió el mismo problema una vez: `VaultSession` es conceptualmente de auth (guarda la clave de desbloqueo) pero vive en `core/database/` porque `features/vault` también lo necesita. Aquí, `core/network/api_client.dart` necesita leer `TokenStore` en cada petición, y `core` no puede depender de `features/auth` sin invertir la dirección de dependencia — misma razón, misma solución.

## Consecuencias

- `client/README.md` y cualquier referencia futura al patrón del cliente deben citar este ADR, no (solo) ADR-005, para la parte de gestión de estado y HTTP.
- Añadir `flutter_riverpod` de verdad sigue siendo una opción legítima para un slice futuro con estado async genuinamente más complejo (motor de sync) — esta decisión no lo descarta para siempre, solo evita adoptarlo especulativamente ahora sobre dos pantallas.
- Toda la superficie de red pasa por `ApiClient`; repositorios y presentación solo ven `ApiException`, nunca `DioException` — reduce a un único punto el riesgo de que un error interno (traza, cabecera cruda) llegue a la UI, sirviendo directamente a la pregunta de §167 "¿se filtran secretos?".
- `SecureTokenStore` en Linux depende de que exista un demonio de keyring (`libsecret`/gnome-keyring o equivalente) en tiempo de ejecución — un fallo de *escritura* (no de lectura, que NexusKeys ya trata como "ausente" con normalidad) debe avisar explícitamente en vez de fallar en silencio en cada arranque, siguiendo el principio de §101 de no fingir soporte que no existe.
