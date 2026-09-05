# Cliente Desktop (Fase 3, slice 1 — login + explorador de solo lectura)

Cliente Flutter/Dart de NexusCloud para Windows y Linux (Android, Fase 4, reutilizará este mismo código — §6). Este primer slice cubre únicamente: login, auto-login, logout, y explorar carpetas de solo lectura. Sync, subida/descarga, sharing/papelera/versiones en el cliente, bandeja del sistema, instalador y auto-actualización quedan para slices posteriores (ver `docs/architecture/decisions/ADR-009-flutter-client-foundation.md`).

Sigue el patrón de Clean Architecture por feature ya usado en [NexusKeys](https://github.com/porrii/NexusKeys) (vault de contraseñas del mismo ecosistema Nexus) — pero **no** todo lo que [ADR-005](../docs/architecture/decisions/ADR-005-multiplatform-strategy.md) daba por sentado resultó ser cierto al inspeccionar el código real de NexusKeys; [ADR-009](../docs/architecture/decisions/ADR-009-flutter-client-foundation.md) corrige eso:

- Clean Architecture por feature: cada carpeta bajo `lib/features/` se divide en `domain` (entidades, interfaces de repositorio), `data` (implementaciones, fuentes de datos) y `presentation` (páginas, widgets).
- `get_it` para inyección de dependencias. **Sin `flutter_riverpod`** (a pesar de que ADR-005 lo mencionaba): NexusKeys lo declara pero no lo usa en ningún archivo — el manejo de estado real es un getter síncrono + `Stream` broadcast no-replay en el dominio, consumido vía `StatefulWidget`/`setState`/`StreamBuilder`. Este cliente sigue esa práctica real, no la aspiracional (ver ADR-009).
- `dio` como cliente HTTP (no `package:http`) — necesario para progreso de subida/descarga en un slice futuro; centraliza también el token Bearer, el mapeo de errores y el backoff de 429 en un único interceptor (`core/network/api_client.dart`).
- `flutter_secure_storage` para el token de sesión (nunca en texto plano, §122) — vive en `core/storage/`, no en `features/auth/`, porque `core/network` necesita leerlo en cada petición. **Sin `local_auth`** en este slice: no tiene implementación real en Linux y no es requisito de §45.

El cliente consume la API REST documentada en [`../openapi.yaml`](../openapi.yaml) / [`../docs/api.md`](../docs/api.md) — no hay ningún acoplamiento directo a la base de datos ni al filesystem del servidor.

## Desarrollo

```
flutter pub get
flutter analyze
flutter test
flutter run -d windows   # o -d linux
```

Requiere un servidor NexusCloud real corriendo y accesible (la URL se introduce en la pantalla de login, nunca es una constante — §48).
