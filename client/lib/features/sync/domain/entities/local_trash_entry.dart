import 'package:equatable/equatable.dart';

/// Una entrada de la papelera local (ADR-013): un archivo que un borrado en
/// remoto propagó hacia local (modo `Ambos`) y que `LocalTrashStore` movió
/// en vez de borrar de verdad. [absolutePath] es su ubicación real dentro de
/// la papelera (con el sello de tiempo delante); [relativeSegments] es la
/// ruta ORIGINAL dentro del par (sin el sello), la misma que se usó al
/// llamar a `moveToTrash` -- necesaria para saber dónde restaurarlo.
class LocalTrashEntry extends Equatable {
  const LocalTrashEntry({
    required this.pairKey,
    required this.relativeSegments,
    required this.deletedAt,
    required this.sizeBytes,
    required this.absolutePath,
  });

  /// `SyncPair.stableKey` del par que originó el borrado -- puede que ya no
  /// corresponda a ningún par configurado (se quitó de la lista después).
  final String pairKey;

  final List<String> relativeSegments;
  final DateTime deletedAt;
  final int sizeBytes;
  final String absolutePath;

  String get displayPath => relativeSegments.join('/');

  @override
  List<Object?> get props =>
      [pairKey, relativeSegments, deletedAt, sizeBytes, absolutePath];
}
