import 'package:equatable/equatable.dart';

import 'directory_entry.dart';
import 'file_entry.dart';

/// Contenido de un directorio (`GET /files?path=`) -- siempre el listado
/// completo de ese nivel, el servidor no pagina esta respuesta.
class DirectoryListing extends Equatable {
  const DirectoryListing({
    required this.directories,
    required this.files,
    this.canUpload = false,
    this.maxUploadSizeBytes,
  });

  final List<DirectoryEntry> directories;
  final List<FileEntry> files;

  /// Solo tiene contenido real para una carpeta compartida
  /// (`SharingRemoteDataSource.listSharedDirectory`, §37/ADR-035): si quien
  /// la mira puede subir a ella. El árbol propio y la papelera lo dejan en
  /// `false` (no se usa ahí) -- es un campo aditivo, igual que en el
  /// servidor (`sharedListingResponse`, que los clientes que no lo conocen
  /// simplemente ignoran).
  final bool canUpload;

  /// Límite de tamaño por archivo para subir aquí, si lo hay (`null` = sin
  /// límite, o [canUpload] es `false`). El servidor ya lo hace cumplir de
  /// todos modos (413 `upload_too_large`); esto es solo para poder
  /// anunciarlo antes de intentar la subida.
  final int? maxUploadSizeBytes;

  bool get isEmpty => directories.isEmpty && files.isEmpty;

  @override
  List<Object?> get props => [directories, files, canUpload, maxUploadSizeBytes];
}
