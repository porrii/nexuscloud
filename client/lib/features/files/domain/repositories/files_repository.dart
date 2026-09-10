import '../entities/directory_listing.dart';
import '../entities/file_entry.dart';
import '../entities/file_version.dart';

/// Progreso de una transferencia (subida o descarga) -- `done`/`total` en
/// bytes. Typedef propio en vez de reutilizar el `ProgressCallback` de
/// `dio`: el dominio no debe conocer el paquete HTTP concreto (ADR-009).
typedef TransferProgress = void Function(int done, int total);

/// Listado de solo lectura, y ahora también transferencia de contenido
/// (ADR-010). El listado deliberadamente NO tiene el patrón Stream+getter
/// de auth (ver `AuthRepository`): no hay mutación local ni varios
/// observadores, un simple `Future` de petición/respuesta es la
/// representación honesta.
abstract interface class FilesRepository {
  Future<DirectoryListing> list(String path);

  /// Sube el archivo local en [localFilePath] como [fileName] dentro de
  /// [parentPath]. Devuelve los metadatos ya creados en el servidor.
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  });

  /// Crea la carpeta [name] dentro de [parentPath]. Idempotente en el
  /// servidor (crear una que ya existe devuelve la misma, sin error). Lo
  /// usa la sincronización bidireccional (slice 13) para materializar los
  /// directorios padre de un archivo local nuevo antes de subirlo: subir a
  /// un `parent_path` sin fila de carpeta deja el archivo "colgado" e
  /// invisible en el listado del padre (confirmado contra el backend
  /// real). No admite `/` en [name] -- hay que crear nivel a nivel.
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  });

  /// Descarga [file] a [saveToPath] y verifica su integridad contra
  /// `file.sha256` (§41 no tiene reanudación todavía -- un corte a medias
  /// sería invisible sin esta comprobación, ADR-010).
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  });

  /// Mueve un archivo a la papelera, o lo borra para siempre si
  /// [permanent] es `true` -- un único método con flag, igual que el
  /// propio servidor modela `DELETE /files/{id}?permanent=true`, en vez
  /// de dos métodos separados.
  Future<void> deleteFile(String fileId, {bool permanent = false});

  /// Igual que [deleteFile], para carpetas. El servidor rechaza borrar
  /// (normal o permanente) una carpeta activa con contenido activo
  /// dentro (`409 not_empty`) -- elementos ya trasheados no cuentan.
  Future<void> deleteDirectory(String directoryId, {bool permanent = false});

  Future<void> restoreFile(String fileId);

  Future<void> restoreDirectory(String directoryId);

  /// Vista plana (sin jerarquía) de todo lo borrado (no permanentemente)
  /// por el usuario -- misma forma que [list], siempre con `deletedAt`
  /// poblado.
  Future<DirectoryListing> listTrash();

  /// Historial de versiones de un archivo (ADR-007), más reciente primero.
  /// Solo los archivos tienen versiones -- no existe el concepto para
  /// carpetas, ni aquí ni en el servidor.
  Future<List<FileVersion>> listVersions(String fileId);

  /// Descarga el contenido de [version] (no el actual) a [saveToPath],
  /// con la misma verificación de integridad que [downloadFile].
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  });

  /// Restaura [versionNum] como el contenido activo del archivo --
  /// no-destructivo por diseño (el contenido activo actual se empuja a su
  /// vez al historial antes de traer de vuelta el antiguo), por lo que no
  /// requiere confirmación en la UI. A diferencia de [restoreFile]
  /// (`204` sin cuerpo), este endpoint sí devuelve el `FileEntry`
  /// actualizado -- se propaga en vez de descartarse.
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  });
}
