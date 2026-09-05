/// Guarda el token de sesión (bearer) de forma segura.
///
/// Vive en `core/` y no en `features/auth/` porque
/// `core/network/api_client.dart` necesita leerlo en cada petición — `core`
/// no puede depender de `features/auth` sin invertir la dirección de
/// dependencia. Mismo razonamiento que NexusKeys aplicó a `VaultSession`
/// (vive en `core/database/` porque más de una feature lo necesita); ver
/// ADR-009.
abstract interface class TokenStore {
  Future<void> save(String token);
  Future<String?> read();
  Future<void> clear();
}
