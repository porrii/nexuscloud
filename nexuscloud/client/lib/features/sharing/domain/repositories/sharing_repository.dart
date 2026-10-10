import '../../../files/domain/entities/directory_listing.dart';
import '../../../files/domain/entities/file_entry.dart';
import '../../../files/domain/repositories/files_repository.dart' show TransferProgress;
import '../entities/group.dart';
import '../entities/share.dart';

abstract interface class SharingRepository {
  Future<List<Group>> listGroups();

  /// Crea una compartición. `canUpload` solo tiene efecto real cuando
  /// [resourceType] es [ShareResourceType.directory] y [shareType] es
  /// [ShareType.link] -- el servidor lo fuerza a `false` en cualquier
  /// otra combinación, sin aviso. `password`/`expiresAt`/`maxDownloads`
  /// solo tienen sentido para [ShareType.link] (el servidor los ignora en
  /// user/group, pero no hace falta que el cliente lo replique: no se
  /// muestran esos campos fuera de la pestaña Enlace).
  Future<Share> createShare({
    required ShareResourceType resourceType,
    required String resourceId,
    required ShareType shareType,
    String? targetUsername,
    String? targetGroupId,
    String? label,
    bool? canDownload,
    bool canUpload = false,
    String? password,
    DateTime? expiresAt,
    int? maxDownloads,
  });

  /// El servidor no filtra por recurso -- para "¿quién tiene acceso a
  /// este archivo?" hay que pedir la lista completa y filtrar en el
  /// cliente por `resourceId`.
  Future<List<Share>> listShares({required ShareDirection direction});

  /// Revocación irreversible (soft-update sin endpoint de "des-revocar").
  Future<void> revokeShare(String shareId);

  /// URL base del servidor tal como el usuario la escribió en el login
  /// (sin el sufijo `/api/v1` que sí lleva la baseUrl interna de `Dio`).
  /// Se usa para construir la URL pública mostrable
  /// `{serverBaseUrl}/s/{token}` tras crear un enlace.
  Future<String?> get serverBaseUrl;

  /// Navega una carpeta compartida por otro usuario -- mismo endpoint
  /// re-autorizado en cada nivel contra el ID real de la (sub)carpeta,
  /// nunca una ruta. Reutiliza `DirectoryListing`/`DirectoryEntry`/
  /// `FileEntry` de `features/files`: el servidor devuelve exactamente la
  /// misma forma que `GET /files`, con metadatos completos por archivo
  /// (así que un archivo encontrado aquí se descarga con
  /// `FilesRepository.downloadFile`, no con [downloadSharedFile]).
  Future<DirectoryListing> listSharedDirectory(String directoryId);

  /// Sube el archivo local en [localFilePath] como [fileName] dentro de la
  /// carpeta compartida [directoryId] -- mismo endpoint que ya usan la web y
  /// la CLI (§37, ADR-035). El destino sale solo del ID de la carpeta: la
  /// autorización (`canUpload` del listado) se vuelve a comprobar en el
  /// servidor en cada petición, nunca se confía en lo que devolvió un
  /// [listSharedDirectory] anterior. Devuelve los metadatos ya creados.
  Future<FileEntry> uploadToSharedDirectory({
    required String directoryId,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  });

  /// Descarga un archivo compartido DIRECTAMENTE conmigo (sin pasar por
  /// una carpeta) -- a diferencia de `FilesRepository.downloadFile`, no
  /// hay un `FileEntry` conocido de antemano (el listado `with-me` solo
  /// trae `resourceName`, sin tamaño/hash: el servidor calcula el tamaño
  /// para construir su propia respuesta interna y lo descarta antes de
  /// serializarla). La verificación de integridad depende únicamente de
  /// `X-Content-SHA256` en la respuesta de descarga (el mismo endpoint
  /// `GET /files/{id}` que ya usa `downloadFile`, con el mismo conjunto de
  /// cabeceras siempre presente según el propio servidor).
  Future<void> downloadSharedFile({
    required String fileId,
    required String saveToPath,
    TransferProgress? onProgress,
  });
}
