import 'package:equatable/equatable.dart';

/// Resultado de una pasada de `SyncEngine.syncNow` -- unidireccional, así
/// que no hay "conflictos" en el sentido de §40, solo descargas, omisiones
/// (ya al día) y errores por archivo (incluidas las colisiones de
/// mayúsculas/minúsculas detectadas antes de descargar nada, ADR-011).
class SyncResult extends Equatable {
  const SyncResult({
    required this.downloaded,
    required this.skipped,
    required this.errors,
    required this.finishedAt,
  });

  final int downloaded;
  final int skipped;
  final List<String> errors;
  final DateTime finishedAt;

  @override
  List<Object?> get props => [downloaded, skipped, errors, finishedAt];
}
