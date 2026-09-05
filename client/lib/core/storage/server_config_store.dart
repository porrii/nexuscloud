/// Recuerda la última URL de servidor usada. NO es secreta — a diferencia
/// del token, no necesita el almacén seguro del SO.
abstract interface class ServerConfigStore {
  Future<void> save(String baseUrl);
  Future<String?> read();
  Future<void> clear();
}
