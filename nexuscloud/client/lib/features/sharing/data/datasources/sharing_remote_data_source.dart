import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/api_exception.dart';
import '../../../files/data/models/directory_entry_model.dart';
import '../../../files/data/models/file_entry_model.dart';
import '../../../files/domain/entities/directory_listing.dart';
import '../../../files/domain/entities/file_entry.dart';
import '../../../files/domain/repositories/files_repository.dart' show TransferProgress;
import '../../domain/entities/group.dart';
import '../../domain/entities/share.dart';
import '../models/group_model.dart';
import '../models/share_model.dart';

class SharingRemoteDataSource {
  SharingRemoteDataSource({required ApiClient apiClient})
      : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// Igual que `/files/{id}/versions`: el servidor devuelve un array JSON
  /// crudo, no un objeto con clave.
  Future<List<Group>> listGroups() async {
    final response =
        await _apiClient.request((dio) => dio.get<List<dynamic>>('/groups'));
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(GroupModel.fromJson)
        .toList();
  }

  /// Mismo patrón de `POST` con cuerpo JSON tipo objeto que ya usa
  /// `AuthRemoteDataSource.login()` -- el interceptor por defecto de
  /// `dio` detecta un `Map` y pone `application/json` solo, sin
  /// configuración adicional.
  ///
  /// `expiresAt` se serializa con `.toUtc()` antes de `toIso8601String()`
  /// -- sin eso, el resultado no lleva marca de zona horaria y el
  /// servidor (que exige RFC3339 estricto) rechaza la petición entera con
  /// `400 invalid_request`.
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
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/shares',
        data: {
          'resource_type': resourceType.wireValue,
          'resource_id': resourceId,
          'share_type': shareType.wireValue,
          if (targetUsername != null && targetUsername.isNotEmpty)
            'target_username': targetUsername,
          'target_group_id': ?targetGroupId,
          if (label != null && label.isNotEmpty) 'label': label,
          'can_download': ?canDownload,
          'can_upload': canUpload,
          if (password != null && password.isNotEmpty) 'password': password,
          if (expiresAt != null)
            'expires_at': expiresAt.toUtc().toIso8601String(),
          'max_downloads': ?maxDownloads,
        },
      ),
    );
    return ShareModel.fromJson(response.data!);
  }

  Future<List<Share>> listShares({required ShareDirection direction}) async {
    final response = await _apiClient.request(
      (dio) => dio.get<List<dynamic>>(
        '/shares',
        queryParameters: {'direction': direction.wireValue},
      ),
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(ShareModel.fromJson)
        .toList();
  }

  Future<void> revokeShare(String shareId) {
    return _apiClient.request((dio) => dio.delete<void>('/shares/$shareId'));
  }

  /// Misma forma que `GET /files` (mismos `directories`/`files`, parseados
  /// con los mismos modelos) -- confirmado en el servidor real: usa los
  /// mismos mappers de respuesta que el listado normal. Además trae
  /// `can_upload`/`max_upload_size_bytes` (§37, ADR-035, campos aditivos en
  /// `sharedListingResponse`), que la web ya usa para decidir si ofrecer el
  /// botón de subir.
  Future<DirectoryListing> listSharedDirectory(String directoryId) async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>('/shared-directories/$directoryId'),
    );
    final data = response.data!;
    final directories = (data['directories'] as List)
        .cast<Map<String, dynamic>>()
        .map(DirectoryEntryModel.fromJson)
        .toList();
    final files = (data['files'] as List)
        .cast<Map<String, dynamic>>()
        .map(FileEntryModel.fromJson)
        .toList();
    return DirectoryListing(
      directories: directories,
      files: files,
      canUpload: data['can_upload'] as bool? ?? false,
      maxUploadSizeBytes: data['max_upload_size_bytes'] as int?,
    );
  }

  /// Mismo patrón exacto que `FilesRemoteDataSource.uploadFile` (cuerpo en
  /// streaming, `Content-Length` obligatorio -- sin él Dio nunca llama a
  /// `onSendProgress` --, y el mismo `receiveTimeout` alargado para lo que
  /// tarde el servidor en escribir+hashear+insertar). La única diferencia es
  /// la ruta: el destino sale del ID de la carpeta compartida, nunca de una
  /// ruta que el cliente elija (§37, ADR-035) -- el servidor re-deriva el
  /// permiso de subida de la base de datos en cada petición.
  Future<FileEntry> uploadToSharedDirectory({
    required String directoryId,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) async {
    final file = File(localFilePath);
    final length = await file.length();
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/shared-directories/$directoryId/files',
        queryParameters: {'name': fileName},
        data: file.openRead(),
        options: Options(
          headers: {Headers.contentLengthHeader: length},
          receiveTimeout: const Duration(minutes: 2),
        ),
        onSendProgress: onProgress,
      ),
    );
    return FileEntryModel.fromJson(response.data!);
  }

  /// Mismo patrón que `FilesRemoteDataSource.downloadFile` (mismo
  /// `deleteOnError` implícito de `dio.download`, mismo borrado explícito
  /// del archivo si el hash no coincide -- el `deleteOnError` de `dio`
  /// solo cubre fallos de transferencia, nunca una discrepancia detectada
  /// después de que la descarga ya terminó bien). La única diferencia
  /// real: sin un hash local de respaldo -- un archivo compartido
  /// directamente (sin pasar por una carpeta) no trae `sha256` en el
  /// listado `with-me`. Si la cabecera faltara (no debería, el servidor
  /// siempre la manda), se omite la verificación en vez de comparar contra
  /// `null` y reportar un `integrity_mismatch` falso.
  Future<void> downloadSharedFile({
    required String fileId,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.download(
        '/files/$fileId',
        saveToPath,
        onReceiveProgress: onProgress,
      ),
    );

    final expectedHash = response.headers.value('x-content-sha256');
    if (expectedHash != null) {
      final downloaded = File(saveToPath);
      final digest = await sha256.bind(downloaded.openRead()).first;
      if (digest.toString() != expectedHash) {
        await downloaded.delete();
        throw const ApiException(
          code: 'integrity_mismatch',
          message: 'El archivo descargado no coincide con el original.',
        );
      }
    }
  }
}
