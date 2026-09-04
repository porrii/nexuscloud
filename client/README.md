# Cliente Desktop/Android (Fase 3-4 — no implementado todavía)

Este directorio está reservado para el cliente Flutter/Dart de NexusCloud (Windows, Linux, Android — §6).

Cuando se implemente, seguirá el mismo patrón ya validado en [NexusKeys](https://github.com/porrii/NexusKeys) (vault de contraseñas del mismo ecosistema Nexus), documentado en [ADR-005](../docs/architecture/decisions/ADR-005-multiplatform-strategy.md):

- Clean Architecture por feature: cada carpeta bajo `lib/features/` se divide en `domain` (entidades, interfaces de repositorio), `data` (implementaciones, fuentes de datos) y `presentation` (páginas, widgets).
- `get_it` para inyección de dependencias, `flutter_riverpod` para estado.
- `flutter_secure_storage` para el token de sesión (nunca en texto plano), `local_auth` donde aplique.
- Estructura de arranque de toolchain en `installation/` (scripts por plataforma), igual que en NexusKeys.

El cliente consumirá la API REST documentada en [`../openapi.yaml`](../openapi.yaml) / [`../docs/api.md`](../docs/api.md) — no hay ningún acoplamiento directo a la base de datos ni al filesystem del servidor.
