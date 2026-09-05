import 'package:equatable/equatable.dart';

import 'directory_entry.dart';
import 'file_entry.dart';

/// Contenido de un directorio (`GET /files?path=`) -- siempre el listado
/// completo de ese nivel, el servidor no pagina esta respuesta.
class DirectoryListing extends Equatable {
  const DirectoryListing({required this.directories, required this.files});

  final List<DirectoryEntry> directories;
  final List<FileEntry> files;

  bool get isEmpty => directories.isEmpty && files.isEmpty;

  @override
  List<Object?> get props => [directories, files];
}
