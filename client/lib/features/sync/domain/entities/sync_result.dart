import 'package:equatable/equatable.dart';

/// Resultado de una pasada de `SyncEngine.syncNow`.
///
/// Desde el slice 13 el motor puede ir en tres sentidos
/// ([SyncDirection]): [downloaded] cuenta lo traído del servidor,
/// [uploaded] lo enviado, [skipped] lo que ya estaba al día, [errors] los
/// fallos por archivo (incluidas las colisiones de mayúsculas/minúsculas
/// detectadas antes de transferir nada, ADR-011), y [conflicts] los
/// archivos que cambiaron en los DOS lados desde la última sincronización:
/// para esos no se sobrescribe nada (§40), se deja una *conflict copy*
/// junto al archivo local y se listan aquí.
class SyncResult extends Equatable {
  const SyncResult({
    required this.downloaded,
    required this.uploaded,
    required this.skipped,
    required this.errors,
    required this.conflicts,
    required this.finishedAt,
  });

  final int downloaded;
  final int uploaded;
  final int skipped;
  final List<String> errors;
  final List<String> conflicts;
  final DateTime finishedAt;

  @override
  List<Object?> get props => [
        downloaded,
        uploaded,
        skipped,
        errors,
        conflicts,
        finishedAt,
      ];
}
