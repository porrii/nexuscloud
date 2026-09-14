import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:equatable/equatable.dart';

/// Un único par carpeta-remota/carpeta-local a sincronizar (slice A: solo
/// se admite uno a la vez -- ver ADR-011).
class SyncPair extends Equatable {
  const SyncPair({required this.remotePath, required this.localPath});

  final String remotePath;
  final String localPath;

  /// Identificador estable y corto del par -- hash SHA-256 (16 hex) de
  /// `remotePath|localPath`. No es un nombre de fichero válido en sí mismo
  /// (rutas pueden llevar caracteres inválidos o ser larguísimas), así que
  /// tanto el manifiesto de estado (`FileSyncStateStore`, ADR-012) como la
  /// papelera local (`FileLocalTrashStore`, ADR-013) lo usan como nombre de
  /// carpeta/fichero -- cambiar de carpeta remota o local ⇒ clave distinta
  /// ⇒ cada uno empieza de cero para el par nuevo.
  String get stableKey =>
      sha256.convert(utf8.encode('$remotePath|$localPath')).toString().substring(0, 16);

  @override
  List<Object?> get props => [remotePath, localPath];
}
