import 'dart:async';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;

import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../files/domain/entities/file_entry.dart';
import '../../../files/domain/repositories/files_repository.dart';
import '../entities/pending_delete.dart';
import '../entities/sync_direction.dart';
import '../entities/sync_pair.dart';
import '../entities/sync_result.dart';
import '../entities/sync_state_entry.dart';
import '../repositories/local_trash_store.dart';
import '../repositories/sync_state_store.dart';

/// Un archivo remoto ya localizado durante el recorrido, junto a su ruta
/// relativa a la raíz configurada (como segmentos, nunca como una sola
/// string con `/` -- ver ADR-011 sobre por qué `package:path` necesita
/// segmentos separados).
class _RemoteFile {
  _RemoteFile({required this.file, required this.relativeSegments});

  final FileEntry file;
  final List<String> relativeSegments;

  /// Clave canónica para casar remoto/local/base: ruta relativa con `/` y
  /// en minúsculas. En minúsculas porque NTFS es insensible a
  /// mayúsculas -- `Foto.jpg` remoto y `foto.jpg` local son el MISMO
  /// archivo en Windows (ADR-011 dec. 4 / ADR-012).
  String get key => relativeSegments.join('/').toLowerCase();
}

/// Un archivo local ya localizado durante el recorrido del árbol local.
class _LocalFile {
  _LocalFile({
    required this.file,
    required this.relativeSegments,
    required this.sizeBytes,
    required this.modifiedUtc,
  });

  final File file;
  final List<String> relativeSegments;
  final int sizeBytes;
  final DateTime modifiedUtc;

  String get key => relativeSegments.join('/').toLowerCase();
}

/// Un borrado detectado durante `_reconcilePass` (ADR-013), recolectado en
/// vez de ejecutado al vuelo -- la guarda anti-"borrado masivo" necesita ver
/// el lote completo antes de decidir si se ejecuta o queda pendiente de
/// confirmación.
class _DeleteCandidate {
  _DeleteCandidate({
    required this.key,
    required this.name,
    required this.relPath,
    required this.direction,
    required this.base,
    this.remote,
    this.local,
  });

  final String key;

  /// Último segmento -- para mensajes de `onStatus`, mismo criterio que el
  /// resto del motor.
  final String name;

  /// Ruta relativa completa, con la capitalización real -- para mostrar en
  /// la confirmación de la UI sin ambigüedad entre archivos del mismo
  /// nombre en carpetas distintas.
  final String relPath;

  final DeleteDirection direction;
  final SyncStateEntry base;

  /// Poblado cuando [direction] es [DeleteDirection.toRemote] (hay que
  /// borrar ESTE remoto).
  final _RemoteFile? remote;

  /// Poblado cuando [direction] es [DeleteDirection.toLocal] (hay que
  /// mover ESTE archivo local a la papelera local).
  final _LocalFile? local;
}

/// Acumulador mutable de una pasada -- las tres direcciones lo rellenan
/// igual, así que el resultado final se construye una sola vez.
class _Tally {
  int downloaded = 0;
  int uploaded = 0;
  int skipped = 0;
  int deletedRemote = 0;
  int deletedLocal = 0;
  final errors = <String>[];
  final conflicts = <String>[];
  final pendingDeletes = <PendingDelete>[];
}

/// Motor de sincronización (ADR-011 slices 3/8, ADR-012 slice 13, ADR-013
/// slice 14). Clase concreta sin interfaz separada -- igual que
/// `BackupService` en NexusKeys: orquesta `FilesRepository` (ya existente)
/// más `dart:io`, no hay una segunda implementación que justifique
/// abstraerla.
///
/// Tres sentidos ([SyncDirection]):
///  - `download`: servidor→local (comportamiento original, sin cambios).
///  - `upload`: local→servidor.
///  - `both`: reconcilia los dos lados en UN único recorrido -- no una
///    bajada seguida de una subida, que dejaría que la bajada pisara una
///    edición local antes de que la subida la viera (§40).
///
/// `both` usa un manifiesto de estado local ([SyncStateStore]) como base
/// para distinguir "cambió en remoto" de "cambió en local" de "cambió en
/// los dos" (conflicto §40 → *conflict copy*, nunca se sobrescribe en
/// silencio). Los tres modos mantienen el manifiesto al día.
///
/// **Borrados (ADR-013, solo en `both`)**: un archivo que SÍ tenía base y
/// desaparece de un lado se propaga al otro -- a la papelera del servidor
/// (ADR-006) o a la papelera local ([LocalTrashStore]), nunca a un borrado
/// definitivo directo. Un lote de más de [_maxAutoDeleteBatch] borrados en
/// una misma pasada NO se ejecuta sin que quien llama a [syncNow] confirme
/// explícitamente (`confirmedDeletePaths`) -- guarda anti-"borrado masivo".
/// `download`/`upload` siguen sin propagar borrados en ningún caso.
class SyncEngine {
  SyncEngine({
    required FilesRepository filesRepository,
    required SyncStateStore stateStore,
    required LocalTrashStore trashStore,
  })  : _filesRepository = filesRepository,
        _stateStore = stateStore,
        _trashStore = trashStore;

  final FilesRepository _filesRepository;
  final SyncStateStore _stateStore;
  final LocalTrashStore _trashStore;

  /// Tolerancia de comparación de fecha de modificación: cubre pequeñas
  /// diferencias de redondeo entre cómo el servidor serializa
  /// `updated_at`, la precisión que el filesystem local conserva al hacer
  /// round-trip por `setLastModified`/`FileStat.modified`, y la que
  /// sobrevive a serializar el manifiesto a ISO-8601.
  static const _mtimeTolerance = Duration(seconds: 2);

  /// Guarda anti-"borrado masivo" (ADR-013): si una pasada de `both`
  /// detecta más borrados que esto (sumando los dos sentidos), NINGUNO se
  /// ejecuta sin confirmación explícita -- se listan en
  /// `SyncResult.pendingDeletes`. Diez es deliberadamente conservador para
  /// uso personal (§160): un borrado suelto o unos pocos son gestos
  /// normales del usuario; un lote grande de golpe huele más a carpeta mal
  /// apuntada o unidad desconectada que a intención real.
  static const _maxAutoDeleteBatch = 10;

  /// Nombres locales que nunca se suben: basura del SO, y las propias
  /// *conflict copies* (si no, una se subiría como "archivo local nuevo" y
  /// se propagaría al servidor).
  static final _conflictCopyPattern =
      RegExp(r' \(conflicto \d{4}-\d{2}-\d{2} \d{2}\.\d{2}\.\d{2}\)');
  static const _osJunkNames = {'.ds_store', 'thumbs.db', 'desktop.ini'};

  Future<SyncResult>? _inFlight;
  final _busyController = StreamController<bool>.broadcast();

  /// Emite `true` justo cuando arranca cualquier sincronización y `false`
  /// justo cuando termina (con éxito o error) -- sin importar si la
  /// arrancó el botón manual de `SyncSettingsPage` o `AutoSyncScheduler`.
  Stream<bool> get onBusyChanged => _busyController.stream;

  bool get isRunning => _inFlight != null;

  /// Deliberadamente NO `async`: el chequeo+asignación de [_inFlight] tiene
  /// que ocurrir en el mismo tramo síncrono, antes de que `_runSync` ceda
  /// el control en su primer `await` interno -- así dos llamadas seguidas
  /// (aunque vengan del mismo evento) ven la guarda de forma consistente.
  ///
  /// Una segunda llamada mientras la primera sigue en curso NO lanza ni se
  /// descarta: se une a la ya-en-curso y recibe el mismo [SyncResult]. Si
  /// esa segunda llamada pedía otra [direction] o [confirmedDeletePaths],
  /// se ignora -- gana la que arrancó; es un caso límite raro y no vale la
  /// pena encolarlo.
  ///
  /// [confirmedDeletePaths] son claves (`PendingDelete.key`) que el usuario
  /// ya confirmó explícitamente en una llamada anterior cuyo resultado trajo
  /// `pendingDeletes` no vacío -- esos borrados se ejecutan sin importar el
  /// tamaño del lote. `AutoSyncScheduler` nunca lo rellena.
  Future<SyncResult> syncNow(
    SyncPair pair, {
    SyncDirection direction = SyncDirection.download,
    void Function(String status)? onStatus,
    Set<String>? confirmedDeletePaths,
  }) {
    final existing = _inFlight;
    if (existing != null) {
      onStatus?.call(
        'Ya hay una sincronización en curso; esperando a que termine...',
      );
      return existing;
    }
    final future = _runSync(
      pair,
      direction,
      confirmedDeletePaths ?? const {},
      onStatus,
    );
    _inFlight = future;
    _busyController.add(true);
    // `.whenComplete(...)` devuelve una Future NUEVA e independiente -- si
    // no se descarta con `.ignore()`, un fallo de arranque dispara un
    // reporte de "error no capturado" de más, encima del error que sí
    // recibe quien de verdad esperaba `syncNow`.
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
    SyncDirection direction,
    Set<String> confirmedDeletePaths,
    void Function(String status)? onStatus,
  ) async {
    final baseline = await _stateStore.read(pair);
    final newBaseline = <String, SyncStateEntry>{};
    final tally = _Tally();

    // El árbol remoto hace falta en los tres modos: `download` para saber
    // qué traer, `upload`/`both` para saber qué hay ya en el servidor. Una
    // raíz remota inexistente propaga la `ApiException` tal cual -- es un
    // fallo de arranque, no un error por archivo.
    final remoteFiles = await _walkRemote(pair.remotePath, onStatus);
    final remoteByKey = <String, _RemoteFile>{};
    final collisionKeys = <String>{};
    _indexRemote(remoteFiles, remoteByKey, collisionKeys, tally);

    switch (direction) {
      case SyncDirection.download:
        await _downloadPass(pair, remoteByKey, newBaseline, tally, onStatus);
      case SyncDirection.upload:
        final localFiles = await _walkLocal(pair.localPath, onStatus);
        await _uploadPass(
          pair,
          localFiles,
          remoteByKey,
          collisionKeys,
          baseline,
          newBaseline,
          tally,
          onStatus,
        );
      case SyncDirection.both:
        // Carpeta local inexistente/inaccesible (ADR-013): con borrados ya
        // activos, tratar "no veo nada en local" como "se borró todo" sería
        // catastrófico (unidad desconectada, ruta mal apuntada...). Se
        // aborta la reconciliación sin tocar nada y se conserva el
        // manifiesto tal cual -- ni se sube ni se baja ni se borra.
        if (!Directory(pair.localPath).existsSync()) {
          tally.errors.add(
            'La carpeta local no existe o no está accesible ahora mismo -- '
            'no se sincroniza en modo Ambos para evitar interpretar esto '
            'como que se borró todo el árbol y propagarlo al servidor.',
          );
          newBaseline.addAll(baseline);
        } else {
          final localFiles = await _walkLocal(pair.localPath, onStatus);
          await _reconcilePass(
            pair,
            remoteByKey,
            localFiles,
            collisionKeys,
            baseline,
            newBaseline,
            tally,
            confirmedDeletePaths,
            onStatus,
          );
        }
    }

    await _stateStore.write(pair, newBaseline);

    return SyncResult(
      downloaded: tally.downloaded,
      uploaded: tally.uploaded,
      skipped: tally.skipped,
      errors: tally.errors,
      conflicts: tally.conflicts,
      deletedRemote: tally.deletedRemote,
      deletedLocal: tally.deletedLocal,
      pendingDeletes: tally.pendingDeletes,
      finishedAt: DateTime.now().toUtc(),
    );
  }

  // --- indexado + colisiones de mayúsculas/minúsculas -----------------

  void _indexRemote(
    List<_RemoteFile> remoteFiles,
    Map<String, _RemoteFile> remoteByKey,
    Set<String> collisionKeys,
    _Tally tally,
  ) {
    final groups = <String, List<_RemoteFile>>{};
    for (final f in remoteFiles) {
      groups.putIfAbsent(f.key, () => []).add(f);
    }
    for (final entry in groups.entries) {
      if (entry.value.length > 1) {
        final names = entry.value.map((f) => f.file.name).join(', ');
        collisionKeys.add(entry.key);
        tally.errors.add(
          'Conflicto de mayúsculas/minúsculas entre: $names '
          '(el servidor los trata como archivos distintos, pero en este '
          'sistema local mapearían al mismo archivo -- ninguno se sincronizó)',
        );
      } else {
        remoteByKey[entry.key] = entry.value.single;
      }
    }
  }

  // --- pasada: solo descarga (servidor→local) ------------------------

  Future<void> _downloadPass(
    SyncPair pair,
    Map<String, _RemoteFile> remoteByKey,
    Map<String, SyncStateEntry> newBaseline,
    _Tally tally,
    void Function(String status)? onStatus,
  ) async {
    for (final remote in remoteByKey.values) {
      final localPath = p.joinAll([pair.localPath, ...remote.relativeSegments]);
      try {
        if (_needsDownload(remote.file, localPath)) {
          onStatus?.call('Descargando ${remote.file.name}...');
          await _download(remote, localPath);
          tally.downloaded++;
        } else {
          tally.skipped++;
        }
        newBaseline[remote.key] = _entryAfterDownload(remote, localPath);
      } on ApiException catch (e) {
        tally.errors.add('${remote.file.name}: ${e.message}');
      } on FileSystemException catch (e) {
        tally.errors.add('${remote.file.name}: ${e.message}');
      }
    }
  }

  // --- pasada: solo subida (local→servidor) -------------------------

  Future<void> _uploadPass(
    SyncPair pair,
    List<_LocalFile> localFiles,
    Map<String, _RemoteFile> remoteByKey,
    Set<String> collisionKeys,
    Map<String, SyncStateEntry> baseline,
    Map<String, SyncStateEntry> newBaseline,
    _Tally tally,
    void Function(String status)? onStatus,
  ) async {
    final ensuredDirs = <String>{};
    for (final local in localFiles) {
      final key = local.key;
      if (collisionKeys.contains(key)) continue;
      final name = local.relativeSegments.last;
      final remote = remoteByKey[key];
      final base = baseline[key];
      try {
        if (remote == null) {
          onStatus?.call('Subiendo $name...');
          final entry = await _upload(pair, local, ensuredDirs);
          newBaseline[key] = _entryAfterUpload(entry);
          tally.uploaded++;
          continue;
        }
        if (_matchesBase(base, local) && _remoteMatchesBase(base, remote.file)) {
          newBaseline[key] = base!;
          tally.skipped++;
          continue;
        }
        if (await _sha(local.file) == remote.file.sha256) {
          newBaseline[key] = _entryInSync(local, remote.file);
          tally.skipped++;
          continue;
        }
        onStatus?.call('Subiendo $name...');
        final entry = await _upload(pair, local, ensuredDirs);
        newBaseline[key] = _entryAfterUpload(entry);
        tally.uploaded++;
      } on ApiException catch (e) {
        tally.errors.add('$name: ${e.message}');
      } on FileSystemException catch (e) {
        tally.errors.add('$name: ${e.message}');
      }
    }
  }

  // --- pasada: reconciliación de tres vías (both) -------------------

  Future<void> _reconcilePass(
    SyncPair pair,
    Map<String, _RemoteFile> remoteByKey,
    List<_LocalFile> localFiles,
    Set<String> collisionKeys,
    Map<String, SyncStateEntry> baseline,
    Map<String, SyncStateEntry> newBaseline,
    _Tally tally,
    Set<String> confirmedDeletePaths,
    void Function(String status)? onStatus,
  ) async {
    final localByKey = {for (final f in localFiles) f.key: f};
    final allKeys = <String>{
      ...remoteByKey.keys,
      ...localByKey.keys,
      ...baseline.keys,
    };
    final ensuredDirs = <String>{};
    final deleteCandidates = <_DeleteCandidate>[];

    for (final key in allKeys) {
      if (collisionKeys.contains(key)) continue;
      final remote = remoteByKey[key];
      final local = localByKey[key];

      if (remote == null && local == null) {
        // Solo estaba en la base: borrado en los dos lados -> se deja caer
        // la entrada del manifiesto (no se copia a newBaseline) y nada más.
        continue;
      }

      final base = baseline[key];
      final segments = local?.relativeSegments ?? remote!.relativeSegments;
      final localPath = p.joinAll([pair.localPath, ...segments]);
      final name = segments.last;

      try {
        if (remote != null && local == null) {
          if (base == null) {
            // Nuevo en remoto -> descargar.
            onStatus?.call('Descargando $name...');
            await _download(remote, localPath);
            newBaseline[key] = _entryAfterDownload(remote, localPath);
            tally.downloaded++;
          } else {
            // Estaba sincronizado y ya no está en local (ADR-013): borrado
            // local que se propaga -> candidato a borrar también en
            // remoto. Se recolecta, no se ejecuta todavía.
            deleteCandidates.add(_DeleteCandidate(
              key: key,
              name: name,
              relPath: segments.join('/'),
              direction: DeleteDirection.toRemote,
              base: base,
              remote: remote,
            ));
          }
        } else if (remote == null && local != null) {
          if (base == null) {
            // Nuevo en local -> subir.
            onStatus?.call('Subiendo $name...');
            final entry = await _upload(pair, local, ensuredDirs);
            newBaseline[key] = _entryAfterUpload(entry);
            tally.uploaded++;
          } else {
            // Estaba sincronizado y ya no está en remoto: candidato a
            // borrar también en local.
            deleteCandidates.add(_DeleteCandidate(
              key: key,
              name: name,
              relPath: segments.join('/'),
              direction: DeleteDirection.toLocal,
              base: base,
              local: local,
            ));
          }
        } else if (remote != null && local != null) {
          await _reconcileBothPresent(
            pair: pair,
            key: key,
            name: name,
            localPath: localPath,
            remote: remote,
            local: local,
            base: base,
            newBaseline: newBaseline,
            tally: tally,
            ensuredDirs: ensuredDirs,
            onStatus: onStatus,
          );
        }
      } on ApiException catch (e) {
        tally.errors.add('$name: ${e.message}');
      } on FileSystemException catch (e) {
        tally.errors.add('$name: ${e.message}');
      }
    }

    await _resolveDeletes(
      pair,
      deleteCandidates,
      confirmedDeletePaths,
      newBaseline,
      tally,
      onStatus,
    );
  }

  Future<void> _reconcileBothPresent({
    required SyncPair pair,
    required String key,
    required String name,
    required String localPath,
    required _RemoteFile remote,
    required _LocalFile local,
    required SyncStateEntry? base,
    required Map<String, SyncStateEntry> newBaseline,
    required _Tally tally,
    required Set<String> ensuredDirs,
    required void Function(String status)? onStatus,
  }) async {
    if (base == null) {
      // Primera vez que se ven los dos y no hay base: si el contenido ya
      // coincide, solo se registra; si no, no hay forma de saber quién
      // manda -> conflicto.
      if (await _sha(local.file) == remote.file.sha256) {
        newBaseline[key] = _entryInSync(local, remote.file);
        tally.skipped++;
      } else {
        await _writeConflictCopy(remote, localPath, tally, name);
        newBaseline[key] = _entryConflictResolved(local, remote.file);
      }
      return;
    }

    final rChanged = !_close(remote.file.updatedAt, base.remoteUpdatedAt) ||
        remote.file.sizeBytes != base.remoteSizeBytes;
    final lChanged = !_close(local.modifiedUtc, base.localModifiedAt) ||
        local.sizeBytes != base.localSizeBytes;

    if (!rChanged && !lChanged) {
      newBaseline[key] = base;
      tally.skipped++;
      return;
    }
    if (rChanged && !lChanged) {
      onStatus?.call('Descargando $name...');
      await _download(remote, localPath);
      newBaseline[key] = _entryAfterDownload(remote, localPath);
      tally.downloaded++;
      return;
    }
    if (!rChanged && lChanged) {
      onStatus?.call('Subiendo $name...');
      final entry = await _upload(pair, local, ensuredDirs);
      newBaseline[key] = _entryAfterUpload(entry);
      tally.uploaded++;
      return;
    }
    // Cambió en los dos lados desde la última sincronización.
    if (await _sha(local.file) == remote.file.sha256) {
      // Convergieron al mismo contenido por su cuenta -- no es conflicto.
      newBaseline[key] = _entryInSync(local, remote.file);
      tally.skipped++;
      return;
    }
    await _writeConflictCopy(remote, localPath, tally, name);
    // Se avanza la base a (remoto actual, local actual): el conflicto se
    // notifica UNA vez y no se re-dispara en cada tick salvo que un lado
    // vuelva a cambiar de verdad. La conflict copy queda como artefacto
    // para que el usuario la funda a mano.
    newBaseline[key] = _entryConflictResolved(local, remote.file);
  }

  // --- borrados (ADR-013) -------------------------------------------

  /// Decide, para el lote completo de [candidates], cuáles se ejecutan y
  /// cuáles quedan pendientes de confirmación. La comparación con
  /// [_maxAutoDeleteBatch] es sobre el TAMAÑO TOTAL del lote de esta
  /// pasada, no sobre lo que quede después de descontar lo ya confirmado
  /// -- si no, confirmar solo 3 de un lote de 12 dejaría que los 9
  /// restantes se colaran igualmente por quedar "bajo el umbral" ellos
  /// solos. Con el lote entero por encima del umbral, SOLO se ejecuta lo
  /// que esté explícitamente en [confirmedDeletePaths]; el resto queda
  /// pendiente sin importar cuántos sean. Un candidato pendiente conserva
  /// su entrada de manifiesto tal cual -- ni se borra ni se resucita --
  /// para que la próxima pasada lo reconozca otra vez como el mismo
  /// candidato.
  Future<void> _resolveDeletes(
    SyncPair pair,
    List<_DeleteCandidate> candidates,
    Set<String> confirmedDeletePaths,
    Map<String, SyncStateEntry> newBaseline,
    _Tally tally,
    void Function(String status)? onStatus,
  ) async {
    if (candidates.isEmpty) return;

    final toExecute = <_DeleteCandidate>[];
    if (candidates.length <= _maxAutoDeleteBatch) {
      toExecute.addAll(candidates);
    } else {
      for (final c in candidates) {
        if (confirmedDeletePaths.contains(c.key)) {
          toExecute.add(c);
        } else {
          newBaseline[c.key] = c.base;
          tally.pendingDeletes.add(
            PendingDelete(key: c.key, displayPath: c.relPath, direction: c.direction),
          );
        }
      }
    }

    for (final c in toExecute) {
      try {
        if (c.direction == DeleteDirection.toRemote) {
          onStatus?.call('Borrando ${c.name} en el servidor...');
          await _filesRepository.deleteFile(c.remote!.file.id, permanent: false);
          tally.deletedRemote++;
        } else {
          onStatus?.call('Moviendo ${c.name} a la papelera local...');
          await _trashStore.moveToTrash(
            pair: pair,
            relativeSegments: c.local!.relativeSegments,
            file: c.local!.file,
          );
          tally.deletedLocal++;
        }
        // No se copia a newBaseline -- ahora los dos lados están
        // genuinamente ausentes, igual que el caso "borrado en los dos
        // lados" de arriba.
      } on ApiException catch (e) {
        tally.errors.add('${c.name}: ${e.message}');
        newBaseline[c.key] = c.base;
      } on FileSystemException catch (e) {
        tally.errors.add('${c.name}: ${e.message}');
        newBaseline[c.key] = c.base;
      }
    }
  }

  // --- operaciones de transferencia --------------------------------

  Future<void> _download(_RemoteFile remote, String localPath) async {
    await Directory(p.dirname(localPath)).create(recursive: true);
    await _filesRepository.downloadFile(
      file: remote.file,
      saveToPath: localPath,
    );
    await File(localPath).setLastModified(remote.file.updatedAt);
  }

  Future<FileEntry> _upload(
    SyncPair pair,
    _LocalFile local,
    Set<String> ensuredDirs,
  ) async {
    final segments = local.relativeSegments;
    await _ensureRemoteDirs(pair.remotePath, segments, ensuredDirs);
    var parentPath = pair.remotePath;
    for (final s in segments.sublist(0, segments.length - 1)) {
      parentPath = RemotePath.join(parentPath, s);
    }
    final entry = await _filesRepository.uploadFile(
      parentPath: parentPath,
      localFilePath: local.file.path,
      fileName: segments.last,
    );
    // Alinea la fecha local con la que el servidor acaba de fijar, para que
    // el atajo tamaño+fecha vuelva a valer en la siguiente pasada.
    await local.file.setLastModified(entry.updatedAt);
    return entry;
  }

  /// Crea las carpetas padre que falten en el servidor, nivel a nivel
  /// (`createDirectory` no admite `/` en el nombre). Idempotente en el
  /// servidor; además se cachea lo ya creado en esta pasada para no
  /// repetir la llamada por cada archivo de una misma carpeta.
  Future<void> _ensureRemoteDirs(
    String rootPath,
    List<String> segments,
    Set<String> ensuredDirs,
  ) async {
    var parent = rootPath;
    for (final dirName in segments.sublist(0, segments.length - 1)) {
      final full = RemotePath.join(parent, dirName);
      if (ensuredDirs.add(full)) {
        await _filesRepository.createDirectory(
          parentPath: parent,
          name: dirName,
        );
      }
      parent = full;
    }
  }

  Future<void> _writeConflictCopy(
    _RemoteFile remote,
    String localPath,
    _Tally tally,
    String name,
  ) async {
    final dir = p.dirname(localPath);
    final stem = p.basenameWithoutExtension(localPath);
    final ext = p.extension(localPath);
    final stamp = _conflictStamp(remote.file.updatedAt.toLocal());
    final copyPath = p.join(dir, '$stem (conflicto $stamp)$ext');

    // Si ya se generó esta misma conflict copy (mismo timestamp remoto), no
    // se vuelve a descargar ni se reescribe.
    if (!File(copyPath).existsSync()) {
      await Directory(dir).create(recursive: true);
      await _filesRepository.downloadFile(
        file: remote.file,
        saveToPath: copyPath,
      );
    }
    tally.conflicts.add(name);
  }

  // --- construcción de entradas de manifiesto ----------------------

  SyncStateEntry _entryAfterDownload(_RemoteFile remote, String localPath) {
    final localStat = File(localPath).statSync();
    return SyncStateEntry(
      remoteSizeBytes: remote.file.sizeBytes,
      localSizeBytes: localStat.size,
      sha256: remote.file.sha256,
      remoteUpdatedAt: remote.file.updatedAt,
      localModifiedAt: localStat.modified.toUtc(),
    );
  }

  SyncStateEntry _entryAfterUpload(FileEntry entry) => SyncStateEntry(
        remoteSizeBytes: entry.sizeBytes,
        localSizeBytes: entry.sizeBytes,
        sha256: entry.sha256,
        remoteUpdatedAt: entry.updatedAt,
        // `_upload` acaba de fijar la fecha local a `entry.updatedAt`.
        localModifiedAt: entry.updatedAt.toUtc(),
      );

  SyncStateEntry _entryInSync(_LocalFile local, FileEntry remote) =>
      SyncStateEntry(
        remoteSizeBytes: remote.sizeBytes,
        localSizeBytes: local.sizeBytes,
        sha256: remote.sha256,
        remoteUpdatedAt: remote.updatedAt,
        localModifiedAt: local.modifiedUtc,
      );

  /// Tras dejar una conflict copy: la base pasa a reflejar el estado ACTUAL
  /// de los dos lados (con sus tamaños distintos), para que el conflicto no
  /// se repita en cada pasada mientras ninguno vuelva a cambiar.
  SyncStateEntry _entryConflictResolved(_LocalFile local, FileEntry remote) =>
      SyncStateEntry(
        remoteSizeBytes: remote.sizeBytes,
        localSizeBytes: local.sizeBytes,
        sha256: remote.sha256,
        remoteUpdatedAt: remote.updatedAt,
        localModifiedAt: local.modifiedUtc,
      );

  // --- helpers de comparación -------------------------------------

  bool _needsDownload(FileEntry file, String localPath) {
    final localFile = File(localPath);
    if (!localFile.existsSync()) return true;
    final stat = localFile.statSync();
    final sizeMatches = stat.size == file.sizeBytes;
    final mtimeMatches = _close(stat.modified.toUtc(), file.updatedAt);
    return !(sizeMatches && mtimeMatches);
  }

  bool _matchesBase(SyncStateEntry? base, _LocalFile local) =>
      base != null &&
      local.sizeBytes == base.localSizeBytes &&
      _close(local.modifiedUtc, base.localModifiedAt);

  bool _remoteMatchesBase(SyncStateEntry? base, FileEntry remote) =>
      base != null &&
      remote.sizeBytes == base.remoteSizeBytes &&
      _close(remote.updatedAt, base.remoteUpdatedAt);

  bool _close(DateTime a, DateTime b) =>
      a.difference(b).abs() <= _mtimeTolerance;

  Future<String> _sha(File file) async =>
      (await sha256.bind(file.openRead()).first).toString();

  String _conflictStamp(DateTime local) {
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}.${two(local.minute)}.${two(local.second)}';
  }

  // --- recorridos ------------------------------------------------

  /// Recorre el árbol remoto desde [rootPath] llamando a
  /// `FilesRepository.list` una vez por carpeta (secuencial -- ADR-011
  /// dec. 5). Un directorio inexistente en la raíz propaga la
  /// `ApiException` tal cual (fallo de arranque).
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

  /// Recorre el árbol local bajo [rootPath]. Carpeta local inexistente ⇒
  /// lista vacía. En `download`/`upload` eso es inofensivo (nada que
  /// subir); en `both` ya no se llega aquí con la raíz ausente -- ver la
  /// guarda en `_runSync` (ADR-013). Salta basura del SO y conflict copies.
  Future<List<_LocalFile>> _walkLocal(
    String rootPath,
    void Function(String status)? onStatus,
  ) async {
    final root = Directory(rootPath);
    if (!root.existsSync()) return const [];

    final result = <_LocalFile>[];
    var seen = 0;
    await for (final entity
        in root.list(recursive: true, followLinks: false)) {
      if (entity is! File) continue;
      final segments = p.split(p.relative(entity.path, from: rootPath));
      if (_isIgnoredLocalName(segments.last)) continue;
      final stat = entity.statSync();
      result.add(
        _LocalFile(
          file: entity,
          relativeSegments: segments,
          sizeBytes: stat.size,
          modifiedUtc: stat.modified.toUtc(),
        ),
      );
      seen++;
      onStatus?.call('Explorando archivos locales... $seen encontrados');
    }
    return result;
  }

  bool _isIgnoredLocalName(String name) {
    if (_osJunkNames.contains(name.toLowerCase())) return true;
    if (_conflictCopyPattern.hasMatch(name)) return true;
    return false;
  }
}
