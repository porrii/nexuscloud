import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'token_store.dart';

/// Implementación real de [TokenStore], respaldada por el almacén seguro
/// del SO (Credential Manager en Windows, Secret Service/libsecret en
/// Linux) — nunca texto plano (§122).
///
/// A diferencia de NexusKeys (donde un fallo de LECTURA de
/// `SecureVaultKeyStore` es un camino normal: "no hay vault guardado, pide
/// la contraseña maestra"), aquí un fallo de ESCRITURA es distinto: en
/// Linux sin un demonio de keyring activo (gnome-keyring, kwallet...),
/// `flutter_secure_storage` puede lanzar al guardar. Fallar en silencio
/// significaría que el auto-login nunca funciona en esa máquina, cada vez,
/// sin explicación (§101: no fingir soporte que no existe) — por eso
/// [save] deja pasar el error hacia quien llama en vez de tragarlo; solo
/// [read] trata un fallo de plataforma como "no hay nada guardado", igual
/// que el precedente de NexusKeys.
class SecureTokenStore implements TokenStore {
  SecureTokenStore({FlutterSecureStorage? storage})
      : _storage = storage ?? const FlutterSecureStorage();

  static const _storageKey = 'nexuscloud.session_token';

  final FlutterSecureStorage _storage;

  @override
  Future<void> save(String token) =>
      _storage.write(key: _storageKey, value: token);

  @override
  Future<String?> read() async {
    try {
      return await _storage.read(key: _storageKey);
    } on PlatformException {
      return null;
    }
  }

  @override
  Future<void> clear() => _storage.delete(key: _storageKey);
}
