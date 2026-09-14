import 'package:equatable/equatable.dart';

/// Una versión histórica de un archivo (ADR-007) -- snapshot de contenido
/// que fue "actual" en algún momento y quedó desplazado por una subida
/// posterior o por una restauración. `versionNum` es un identificador
/// opaco creciente (no necesariamente contiguo: purgas por límite y
/// restauraciones pueden dejar huecos), nunca un índice 1..N fiable. No
/// trae "quién" lo creó -- el servidor tampoco lo registra.
class FileVersion extends Equatable {
  const FileVersion({
    required this.versionNum,
    required this.sizeBytes,
    required this.sha256,
    required this.mimeType,
    required this.createdAt,
  });

  final int versionNum;
  final int sizeBytes;
  final String sha256;
  final String mimeType;
  final DateTime createdAt;

  @override
  List<Object?> get props =>
      [versionNum, sizeBytes, sha256, mimeType, createdAt];
}
