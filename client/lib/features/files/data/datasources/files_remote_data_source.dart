import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:path/path.dart' as p;

import '../../../../core/network/api_client.dart';
import '../../../../core/network/api_exception.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/entities/file_version.dart';
import '../../domain/repositories/files_repository.dart' show TransferProgress;
import '../models/directory_entry_model.dart';
import '../models/file_entry_model.dart';
import '../models/file_version_model.dart';

class FilesRemoteDataSource {
  FilesRemoteDataSource({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  Future<DirectoryListing> list(String path) async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>(
        '/files',
        queryParameters: {'path': path},
      ),
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
    return DirectoryListing(directories: directories, files: files);
  }

  /// El cuerpo se manda como `Stream` (`file.openRead()`), no cargado
  /// entero en memoria con `readAsBytes()` -- pero eso hace que este
  /// cuerpo NO sea seguro de reintentar automáticamente; ver el chequeo
  /// `is! Stream` en `ApiClient._onError` (ADR-010).
  ///
  /// `Content-Length` es obligatorio, no cosmético: sin él, Dio nunca
  /// llama a `onSendProgress` (ni una vez, ni con un valor indeterminado)
  /// -- confirmado en el código fuente de Dio, no es una suposición.
  ///
  /// `receiveTimeout` se alarga aquí porque el reloj de espera de
  /// respuesta empieza a contar DESPUÉS de mandar el archivo entero, y
  /// cubre lo que tarde el servidor en escribir+hashear+insertar -- el
  /// default global (30s, pensado para respuestas JSON pequeñas) podría
  /// hacer fallar en el cliente una subida grande que el servidor
  /// completa bien segundos después.
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) async {
    final file = File(localFilePath);
    final length = await file.length();
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/files',
        queryParameters: {'name': fileName, 'path': parentPath},
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

  /// Descarga con reanudación transparente (ADR-010, §41): escribe SIEMPRE
  /// a un fichero temporal `<saveToPath>.<hash8>.part` -- nunca directo al
  /// destino final -- y solo lo renombra a [saveToPath] tras verificar su
  /// integridad completa. Si ya existe un `.part` con el MISMO hash
  /// esperado (de un intento anterior cortado por red o cierre de la
  /// app), reanuda pidiendo `Range: bytes=<tamaño actual>-`
  /// (`fileAccessMode: append`, ambos ya soportados por Dio 5.11.1 sin
  /// dependencia nueva); si existe uno con OTRO hash (el remoto cambió de
  /// contenido mientras tanto), lo descarta y empieza de cero. Usado por
  /// [downloadFile]/[downloadVersion] -- comparten exactamente este
  /// mecanismo, solo cambian la URL y el hash esperado.
  ///
  /// A diferencia de antes de este slice, [expectedHash] es el que YA se
  /// conocía antes de empezar (de `file.sha256`/`version.sha256`), no el
  /// de la cabecera `X-Content-SHA256` de la respuesta -- hace falta
  /// saberlo de antemano para poder nombrar el `.part` y decidir si
  /// reanudar. La verificación de integridad final sigue atrapando
  /// cualquier corrupción real igual que antes, sea cual sea la causa.
  Future<void> _downloadResumable({
    required String url,
    required String saveToPath,
    required String expectedHash,
    TransferProgress? onProgress,
  }) async {
    final hash8 =
        expectedHash.length >= 8 ? expectedHash.substring(0, 8) : expectedHash;
    final partPath = '$saveToPath.$hash8.part';
    await _discardStalePartFiles(saveToPath: saveToPath, keepName: p.basename(partPath));

    Future<void> attempt({required bool retryOnRangeError}) async {
      final partFile = File(partPath);
      final alreadyHave = await partFile.exists() ? await partFile.length() : 0;
      try {
        await _apiClient.request(
          (dio) => dio.download(
            url,
            partPath,
            onReceiveProgress: onProgress,
            // A diferencia de antes: un corte de red deja el `.part` tal
            // cual, con el progreso hecho hasta ese punto, en vez de
            // borrarlo -- ahora SÍ hay algo que reanudar la próxima vez.
            deleteOnError: false,
            fileAccessMode:
                alreadyHave > 0 ? FileAccessMode.append : FileAccessMode.write,
            options: alreadyHave > 0
                ? Options(headers: {'range': 'bytes=$alreadyHave-'})
                : null,
          ),
        );
      } on ApiException catch (e) {
        // Por `statusCode`, no por `code`: una petición `dio.download()`
        // (`ResponseType.stream`) nunca llega a exponer el `code` real
        // del sobre de error del servidor (ver ApiException.unknown) --
        // el status HTTP crudo sigue siendo fiable.
        //
        // El offset que teníamos guardado ya no es válido (el remoto
        // encogió/cambió) -- descarta el .part y reintenta UNA vez desde
        // cero, sin que quien llama vea este error intermedio.
        if (e.statusCode == 416 && retryOnRangeError) {
          if (await partFile.exists()) await partFile.delete();
          await attempt(retryOnRangeError: false);
          return;
        }
        rethrow;
      }
    }

    await attempt(retryOnRangeError: true);

    final partFile = File(partPath);
    final digest = await sha256.bind(partFile.openRead()).first;
    if (digest.toString() != expectedHash) {
      await partFile.delete();
      throw const ApiException(
        code: 'integrity_mismatch',
        message: 'El archivo descargado no coincide con el original.',
      );
    }
    await partFile.rename(saveToPath);
  }

  /// Borra cualquier `.part` de un intento anterior para [saveToPath]
  /// salvo el que corresponde al hash que se va a descargar ahora
  /// ([keepName]) -- si el archivo remoto cambió de contenido entre un
  /// corte y este intento, nunca se reanuda con un offset que ya no
  /// corresponde a lo que hay que descargar.
  Future<void> _discardStalePartFiles({
    required String saveToPath,
    required String keepName,
  }) async {
    final dir = Directory(p.dirname(saveToPath));
    if (!await dir.exists()) return;
    final baseName = p.basename(saveToPath);
    await for (final entity in dir.list()) {
      if (entity is! File) continue;
      final name = p.basename(entity.path);
      if (name == keepName) continue;
      if (name.startsWith('$baseName.') && name.endsWith('.part')) {
        await entity.delete();
      }
    }
  }

  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) {
    return _downloadResumable(
      url: '/files/${file.id}',
      saveToPath: saveToPath,
      expectedHash: file.sha256,
      onProgress: onProgress,
    );
  }

  /// `POST /directories` con `{parent_path, name}`. El servidor es
  /// idempotente (crear una carpeta ya existente responde `201` con el
  /// mismo id), así que no hace falta comprobar antes si existe ni
  /// interpretar un conflicto -- basta con llamarlo.
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  }) {
    return _apiClient.request(
      (dio) => dio.post<void>(
        '/directories',
        data: {'parent_path': parentPath, 'name': name},
      ),
    );
  }

  /// El parámetro `permanent` solo se manda cuando es `true` -- igual que
  /// el cliente web, que nunca lo incluye para un borrado normal. El
  /// servidor compara la query string como string exacta (`"true"`), así
  /// que no tiene sentido mandar `"false"` tampoco.
  Future<void> deleteFile(String fileId, {bool permanent = false}) {
    return _apiClient.request(
      (dio) => dio.delete<void>(
        '/files/$fileId',
        queryParameters: permanent ? {'permanent': 'true'} : null,
      ),
    );
  }

  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) {
    return _apiClient.request(
      (dio) => dio.delete<void>(
        '/directories/$directoryId',
        queryParameters: permanent ? {'permanent': 'true'} : null,
      ),
    );
  }

  Future<void> restoreFile(String fileId) {
    return _apiClient.request((dio) => dio.post<void>('/files/$fileId/restore'));
  }

  Future<void> restoreDirectory(String directoryId) {
    return _apiClient.request(
      (dio) => dio.post<void>('/directories/$directoryId/restore'),
    );
  }

  Future<DirectoryListing> listTrash() async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>('/trash'),
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
    return DirectoryListing(directories: directories, files: files);
  }

  /// El servidor devuelve un array JSON crudo (sin envoltorio `{...}`),
  /// más reciente primero -- de ahí `List<dynamic>` y no `Map<String,
  /// dynamic>` como el resto de listados de esta clase.
  Future<List<FileVersion>> listVersions(String fileId) async {
    final response = await _apiClient.request(
      (dio) => dio.get<List<dynamic>>('/files/$fileId/versions'),
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(FileVersionModel.fromJson)
        .toList();
  }

  /// Misma lógica exacta que [downloadFile] (mismo mecanismo de
  /// reanudación vía [_downloadResumable]), pero contra el contenido de
  /// una versión concreta en vez del actual.
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) {
    return _downloadResumable(
      url: '/files/$fileId/versions/${version.versionNum}',
      saveToPath: saveToPath,
      expectedHash: version.sha256,
      onProgress: onProgress,
    );
  }

  /// A diferencia de `restoreFile`/`restoreDirectory` (`204` sin cuerpo),
  /// este endpoint sí manda el `FileEntry` actualizado -- se propaga en
  /// vez de descartarse (ver doc en `FilesRepository.restoreVersion`).
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/files/$fileId/versions/$versionNum/restore',
      ),
    );
    return FileEntryModel.fromJson(response.data!);
  }
}
