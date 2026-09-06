import 'package:equatable/equatable.dart';

/// Metadatos de un archivo, tal como los expone el servidor
/// (`fileResponse` en `internal/api/v1/dto.go`). Este slice solo LEE
/// nombre/tamaño/fecha en el listado -- `sha256` se incluye porque forma
/// parte del recurso real y un slice de descarga lo necesitará para
/// verificar integridad (`X-Content-SHA256`), no porque se use todavía.
class FileEntry extends Equatable {
  const FileEntry({
    required this.id,
    required this.parentPath,
    required this.name,
    required this.sizeBytes,
    required this.sha256,
    required this.mimeType,
    required this.createdAt,
    required this.updatedAt,
    this.deletedAt,
  });

  final String id;
  final String parentPath;
  final String name;
  final int sizeBytes;
  final String sha256;
  final String mimeType;
  final DateTime createdAt;
  final DateTime updatedAt;

  /// Solo poblado cuando este archivo viene de `FilesRepository.listTrash`
  /// -- `null` en un listado normal.
  final DateTime? deletedAt;

  @override
  List<Object?> get props => [
        id,
        parentPath,
        name,
        sizeBytes,
        sha256,
        mimeType,
        createdAt,
        updatedAt,
        deletedAt,
      ];
}
