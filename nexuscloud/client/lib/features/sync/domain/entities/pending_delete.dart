import 'package:equatable/equatable.dart';

/// Hacia qué lado se propagaría un borrado detectado (ADR-013): el lado que
/// aparece en el nombre es el que PERDERÍA el archivo.
enum DeleteDirection {
  /// Desapareció en local (con base) -> se borraría también en el
  /// servidor (a la papelera del servidor, ADR-006).
  toRemote,

  /// Desapareció en remoto (con base) -> se borraría también en local
  /// (movido a la papelera local, ADR-013).
  toLocal,
}

/// Un borrado detectado que NO se ejecutó todavía porque el lote de la
/// pasada superaba `SyncEngine._maxAutoDeleteBatch` -- la guarda
/// anti-"borrado masivo" de ADR-013. Quien llama a `syncNow` debe mostrar
/// la lista real al usuario y, si confirma, volver a llamar pasando las
/// claves (`key`) en `confirmedDeletePaths`.
class PendingDelete extends Equatable {
  const PendingDelete({
    required this.key,
    required this.displayPath,
    required this.direction,
  });

  /// Clave canónica interna (ruta relativa con `/`, en minúsculas) -- la
  /// misma que espera `confirmedDeletePaths`. No pensada para mostrar al
  /// usuario tal cual (usa [displayPath] para eso).
  final String key;

  /// Ruta relativa con la capitalización real del archivo, para mostrar en
  /// la UI.
  final String displayPath;

  final DeleteDirection direction;

  @override
  List<Object?> get props => [key, displayPath, direction];
}
