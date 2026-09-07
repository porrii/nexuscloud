import 'dart:async';
import 'dart:io';

import 'package:path/path.dart' as p;

import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../files/domain/entities/file_entry.dart';
import '../../../files/domain/repositories/files_repository.dart';
import '../entities/sync_pair.dart';
import '../entities/sync_result.dart';

/// Un archivo remoto ya localizado durante el recorrido, junto a su ruta
/// relativa a la raíz configurada (como segmentos, nunca como una sola
/// string con `/` -- ver ADR-011 sobre por qué `package:path` necesita
/// segmentos separados).
class _RemoteFile {
  _RemoteFile({required this.file, required this.relativeSegments});

  final FileEntry file;
  final List<String> relativeSegments;

  String get lowercaseKey => relativeSegments.join('/').toLowerCase();
}

/// Motor de sincronización unidireccional (servidor→local), manual, de un
/// único par de carpetas (ADR-011). Clase concreta sin interfaz separada
/// -- igual que `BackupService` en NexusKeys: orquesta `FilesRepository`
/// (ya existente) más `dart:io`, no hay una segunda implementación que
/// justifique abstraerla.
///
/// No mantiene ningún estado propio entre pasadas: cada `syncNow` decide
/// qué descargar comparando tamaño+fecha de modificación local contra lo
/// que el servidor ya reporta en cada listado (`sizeBytes`/`updatedAt`).
/// Es el propio motor quien fija esa fecha local tras cada descarga
/// (`File.setLastModified`), así que compararla en la siguiente pasada es
/// válido -- no es una suposición sobre un reloj o herramienta ajena.
class SyncEngine {
  SyncEngine({required FilesRepository filesRepository})
      : _filesRepository = filesRepository;

  final FilesRepository _filesRepository;

  /// Tolerancia de comparación de fecha de modificación: cubre pequeñas
  /// diferencias de redondeo entre cómo el servidor serializa
  /// `updated_at` y la precisión que el filesystem local conserva al
  /// hacer round-trip por `setLastModified`/`FileStat.modified`.
  static const _mtimeTolerance = Duration(seconds: 2);

  Future<SyncResult>? _inFlight;
  final _busyController = StreamController<bool>.broadcast();

  /// Emite `true` justo cuando arranca cualquier sincronización y `false`
  /// justo cuando termina (con éxito o error) -- sin importar si la
  /// arrancó el botón manual de `SyncSettingsPage` o `AutoSyncScheduler`.
  /// Existe para que cualquier UI pueda deshabilitar sus propias acciones
  /// de sync mientras haya una en curso, la haya arrancado quien la haya
  /// arrancado -- ver [syncNow] sobre por qué esto no es opcional.
  Stream<bool> get onBusyChanged => _busyController.stream;

  bool get isRunning => _inFlight != null;

  /// Deliberadamente NO `async`: el chequeo+asignación de [_inFlight]
  /// tiene que ocurrir en el mismo tramo síncrono, antes de que
  /// `_runSync` ceda el control en su primer `await` interno -- así dos
  /// llamadas seguidas (aunque vengan del mismo evento) ven la guarda de
  /// forma consistente, sin ninguna ventana de carrera en el propio
  /// chequeo.
  ///
  /// Una segunda llamada mientras la primera sigue en curso NO lanza una
  /// excepción ni se descarta: se une a la ya-en-curso y recibe el mismo
  /// [SyncResult] cuando termine -- ni duplica trabajo de red ni escribe
  /// dos veces al mismo archivo local. Sin esta guarda, un disparador
  /// automático (`AutoSyncScheduler`) solapado con un sync manual, o dos
  /// ticks automáticos seguidos si uno tarda más que el intervalo,
  /// recorrerían el árbol remoto por duplicado y podrían escribir al
  /// mismo archivo local a la vez.
  ///
  /// Coste aceptado: quien se une no recibe los `onStatus` de la
  /// ejecución a la que se unió (solo la primera llamada los recibe) --
  /// razonable para un caso límite raro, no vale la pena una difusión
  /// multi-listener para esto.
  Future<SyncResult> syncNow(
    SyncPair pair, {
    void Function(String status)? onStatus,
  }) {
    final existing = _inFlight;
    if (existing != null) {
      onStatus?.call(
        'Ya hay una sincronización en curso; esperando a que termine...',
      );
      return existing;
    }
    final future = _runSync(pair, onStatus);
    _inFlight = future;
    _busyController.add(true);
    // `.whenComplete(...)` devuelve una Future NUEVA e independiente, no
    // la misma que ya se le devuelve a quien llama -- si no se descarta
    // con `.ignore()`, un fallo de arranque (p.ej. ruta remota
    // inexistente) dispara un reporte de "error no capturado" de más en
    // la zona, encima del error que sí recibe correctamente quien de
    // verdad esperaba `syncNow`.
    future
        .whenComplete(() {
          _inFlight = null;
          _busyController.add(false);
        })
        .ignore();
    return future;
  }

  void dispose() => _busyController.close();

  Future<SyncResult> _runSync(
    SyncPair pair,
    void Function(String status)? onStatus,
  ) async {
    final remoteFiles = await _walkRemote(pair.remotePath, onStatus);

    final groups = <String, List<_RemoteFile>>{};
    for (final remoteFile in remoteFiles) {
      groups.putIfAbsent(remoteFile.lowercaseKey, () => []).add(remoteFile);
    }

    var downloaded = 0;
    var skipped = 0;
    final errors = <String>[];

    for (final group in groups.values) {
      if (group.length > 1) {
        final names = group.map((f) => f.file.name).join(', ');
        errors.add(
          'Conflicto de mayúsculas/minúsculas entre: $names '
          '(el servidor los trata como archivos distintos, pero en este '
          'sistema local mapearían al mismo archivo -- ninguno se descargó)',
        );
        continue;
      }

      final remoteFile = group.single;
      final localPath = p.joinAll([pair.localPath, ...remoteFile.relativeSegments]);

      try {
        if (!_needsDownload(remoteFile.file, localPath)) {
          skipped++;
          continue;
        }
        onStatus?.call('Descargando ${remoteFile.file.name}...');
        await Directory(p.dirname(localPath)).create(recursive: true);
        await _filesRepository.downloadFile(
          file: remoteFile.file,
          saveToPath: localPath,
        );
        await File(localPath).setLastModified(remoteFile.file.updatedAt);
        downloaded++;
      } on ApiException catch (e) {
        errors.add('${remoteFile.file.name}: ${e.message}');
      } on FileSystemException catch (e) {
        // Cubre, entre otros casos, nombres reservados de Windows (CON,
        // PRN...) que el servidor no rechaza (su validación es más
        // estrecha que "caracteres inválidos de Windows", §179) -- el SO
        // local lanza aquí con un mensaje menos amable, pero no aborta
        // el resto de la sincronización (ADR-011, gap aceptado).
        errors.add('${remoteFile.file.name}: ${e.message}');
      }
    }

    return SyncResult(
      downloaded: downloaded,
      skipped: skipped,
      errors: errors,
      finishedAt: DateTime.now().toUtc(),
    );
  }

  bool _needsDownload(FileEntry file, String localPath) {
    final localFile = File(localPath);
    if (!localFile.existsSync()) return true;
    final stat = localFile.statSync();
    final sizeMatches = stat.size == file.sizeBytes;
    final mtimeMatches =
        stat.modified.difference(file.updatedAt).abs() <= _mtimeTolerance;
    return !(sizeMatches && mtimeMatches);
  }

  /// Recorre el árbol remoto desde [rootPath] llamando a
  /// `FilesRepository.list` una vez por carpeta (secuencial -- mismo
  /// criterio que la subida por lotes del slice 2, ADR-010: el rate
  /// limit del servidor tiene margen de sobra a la escala de §160, así
  /// que no hay razón técnica real para paralelizar). Un directorio
  /// inexistente en la raíz configurada propaga la `ApiException` tal
  /// cual -- eso es un fallo de arranque, no un error por archivo, y
  /// quien llama a `syncNow` debe tratarlo aparte del `SyncResult`.
  Future<List<_RemoteFile>> _walkRemote(
    String rootPath,
    void Function(String status)? onStatus,
  ) async {
    final rootSegments = RemotePath.segments(rootPath);
    final result = <_RemoteFile>[];
    var directoriesExplored = 0;

    Future<void> walk(String path) async {
      final listing = await _filesRepository.list(path);
      directoriesExplored++;
      onStatus?.call(
        'Explorando carpetas remotas... $directoriesExplored encontradas',
      );

      for (final file in listing.files) {
        final fullPath = RemotePath.join(file.parentPath, file.name);
        final segments = RemotePath.segments(fullPath);
        result.add(
          _RemoteFile(
            file: file,
            relativeSegments: segments.sublist(rootSegments.length),
          ),
        );
      }
      for (final directory in listing.directories) {
        final fullPath = RemotePath.join(directory.parentPath, directory.name);
        await walk(fullPath);
      }
    }

    await walk(rootPath);
    return result;
  }
}
