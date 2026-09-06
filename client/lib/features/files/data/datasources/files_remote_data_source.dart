import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/api_exception.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/repositories/files_repository.dart' show TransferProgress;
import '../models/directory_entry_model.dart';
import '../models/file_entry_model.dart';

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

  /// `dio.download` escribe directo a disco (nunca carga el archivo
  /// completo en memoria) y ya borra el archivo parcial si algo falla
  /// (`deleteOnError: true` es su valor por defecto -- no hace falta
  /// reimplementarlo aquí).
  ///
  /// Tras completar, verifica integridad contra `X-Content-SHA256` (o
  /// `file.sha256` si la cabecera faltara) -- no hay `Range`/reanudación
  /// todavía (§41), así que un corte a medias sería invisible sin esto.
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.download(
        '/files/${file.id}',
        saveToPath,
        onReceiveProgress: onProgress,
      ),
    );

    final expectedHash =
        response.headers.value('x-content-sha256') ?? file.sha256;
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
}
