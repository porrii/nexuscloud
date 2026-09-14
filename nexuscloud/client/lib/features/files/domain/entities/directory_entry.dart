import 'package:equatable/equatable.dart';

/// Metadatos de una carpeta, tal como los expone el servidor
/// (`directoryResponse` en `internal/api/v1/dto.go`).
class DirectoryEntry extends Equatable {
  const DirectoryEntry({
    required this.id,
    required this.parentPath,
    required this.name,
    required this.createdAt,
    this.deletedAt,
  });

  final String id;
  final String parentPath;
  final String name;
  final DateTime createdAt;

  /// Solo poblado cuando esta carpeta viene de `FilesRepository.listTrash`
  /// -- `null` en un listado normal.
  final DateTime? deletedAt;

  @override
  List<Object?> get props => [id, parentPath, name, createdAt, deletedAt];
}
