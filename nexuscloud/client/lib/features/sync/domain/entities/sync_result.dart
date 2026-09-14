import 'package:equatable/equatable.dart';

import 'pending_delete.dart';

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
///
/// Desde el slice 14 (ADR-013), en modo `Ambos`: [deletedRemote] cuenta los
/// archivos borrados localmente y propagados a la papelera del servidor,
/// [deletedLocal] los borrados en remoto y propagados a la papelera local.
/// [pendingDeletes] lista los borrados detectados que NO se ejecutaron
/// porque el lote superaba el umbral anti-"borrado masivo" -- quien llama
/// debe mostrarlos al usuario y, si los confirma, invocar `syncNow` de
/// nuevo con sus claves en `confirmedDeletePaths`.
///
/// [moved] (ADR-030, §85): un archivo que desapareció de un lado y
/// reapareció en el MISMO lado con idéntico contenido (mismo SHA-256) es
/// un renombrado/movido, no un borrado seguido de una subida/descarga
/// independiente -- ver `SyncEngine._detectAndApplyMoves`. Ni cuenta como
/// borrado (nunca entra en el umbral anti-"borrado masivo", ADR-013) ni
/// como subida/descarga (nunca vuelve a transferir el contenido).
class SyncResult extends Equatable {
  const SyncResult({
    required this.downloaded,
    required this.uploaded,
    required this.skipped,
    required this.moved,
    required this.errors,
    required this.conflicts,
    required this.deletedRemote,
    required this.deletedLocal,
    required this.pendingDeletes,
    required this.finishedAt,
  });

  final int downloaded;
  final int uploaded;
  final int skipped;
  final int moved;
  final List<String> errors;
  final List<String> conflicts;
  final int deletedRemote;
  final int deletedLocal;
  final List<PendingDelete> pendingDeletes;
  final DateTime finishedAt;

  @override
  List<Object?> get props => [
        downloaded,
        uploaded,
        skipped,
        moved,
        errors,
        conflicts,
        deletedRemote,
        deletedLocal,
        pendingDeletes,
        finishedAt,
      ];
}
