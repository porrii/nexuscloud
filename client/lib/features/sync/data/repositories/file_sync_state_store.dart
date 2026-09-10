import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_state_entry.dart';
import '../../domain/repositories/sync_state_store.dart';

/// Manifiesto de estado como **un fichero JSON por par**, bajo el directorio
/// de datos de la app (`getApplicationSupportDirectory()/sync_state/`), no
/// dentro de la carpeta sincronizada (ADR-012): ahí se auto-sincronizaría y
/// el usuario podría borrarlo por error.
///
/// Un fichero por par en vez de una base de datos (`sqflite`/`drift`)
/// porque a la escala de §160 (uso personal) el manifiesto entero cabe
/// holgadamente en memoria y se reescribe entero en cada sync -- una BD
/// solo añadiría una dependencia nativa pesada sin resolver ningún problema
/// real todavía.
class FileSyncStateStore implements SyncStateStore {
  /// Inyectable solo para tests -- así no dependen de `path_provider` ni de
  /// un directorio real de la plataforma. En producción es `null` y se usa
  /// `getApplicationSupportDirectory()`.
  FileSyncStateStore({Directory? baseDirectoryOverride})
      : _baseDirectoryOverride = baseDirectoryOverride;

  final Directory? _baseDirectoryOverride;

  static const _manifestVersion = 1;

  Future<Directory> _stateDir() async {
    final base = _baseDirectoryOverride ?? await getApplicationSupportDirectory();
    return Directory(p.join(base.path, 'sync_state'));
  }

  /// Nombre de fichero derivado del par -- hash para no meter rutas (que
  /// pueden llevar caracteres inválidos para un nombre de fichero, o ser
  /// larguísimas) en el nombre. Cambiar de carpeta remota o local ⇒ hash
  /// distinto ⇒ manifiesto nuevo, empezando de cero.
  String _fileNameFor(SyncPair pair) {
    final raw = '${pair.remotePath}|${pair.localPath}';
    final digest = sha256.convert(utf8.encode(raw)).toString();
    return '${digest.substring(0, 16)}.json';
  }

  Future<File> _fileFor(SyncPair pair) async =>
      File(p.join((await _stateDir()).path, _fileNameFor(pair)));

  @override
  Future<Map<String, SyncStateEntry>> read(SyncPair pair) async {
    final file = await _fileFor(pair);
    if (!file.existsSync()) return {};
    try {
      final decoded = jsonDecode(await file.readAsString());
      if (decoded is! Map) return {};
      final entries = decoded['entries'];
      if (entries is! Map) return {};
      final result = <String, SyncStateEntry>{};
      entries.forEach((key, value) {
        if (key is String && value is Map) {
          result[key] = SyncStateEntry.fromJson(
            Map<String, dynamic>.from(value),
          );
        }
      });
      return result;
    } catch (_) {
      // Manifiesto corrupto/ilegible: degrada a "primera pasada" en vez de
      // romper la sincronización. La próxima escritura lo deja bien.
      return {};
    }
  }

  @override
  Future<void> write(
    SyncPair pair,
    Map<String, SyncStateEntry> entries,
  ) async {
    final dir = await _stateDir();
    await dir.create(recursive: true);
    final file = File(p.join(dir.path, _fileNameFor(pair)));

    final payload = <String, dynamic>{
      'version': _manifestVersion,
      'entries': {
        for (final e in entries.entries) e.key: e.value.toJson(),
      },
    };

    // tmp + rename para no dejar nunca un manifiesto a medio escribir si el
    // proceso muere durante la escritura -- mismo criterio de escritura
    // atómica que el resto del proyecto (ADR-002, ADR-011).
    final tmp = File('${file.path}.tmp');
    await tmp.writeAsString(jsonEncode(payload), flush: true);
    await tmp.rename(file.path);
  }
}
