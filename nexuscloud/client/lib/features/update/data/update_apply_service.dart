import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../../../core/network/api_client.dart';
import '../domain/entities/update_asset.dart';

/// Se lanza si la descarga no coincide con el hash esperado, o si
/// `Update.exe apply` termina con un código de salida distinto de cero.
class UpdateApplyException implements Exception {
  UpdateApplyException(this.message);
  final String message;

  @override
  String toString() => message;
}

/// Descarga y aplica una actualización usando el `Update.exe` que Velopack
/// ya coloca junto a la instalación (ADR-032) -- ni FFI ni SDK, es un
/// subproceso: `Update.exe apply` es el único paso documentado y estable
/// de la CLI de Velopack (confirmado ejecutando `Update.exe apply --help`
/// contra un paquete real durante el desarrollo, no supuesto de memoria).
class UpdateApplyService {
  UpdateApplyService({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// La instalación real es `<raíz>\current\nexuscloud_client.exe`, con
  /// `Update.exe` como hermano de `current\` (verificado instalando de
  /// verdad un paquete Velopack de prueba). En modo desarrollo
  /// (`flutter run`) esa ruta simplemente no existe -- [canApplyUpdates]
  /// cubre ese caso para que la UI no ofrezca "reiniciar y actualizar"
  /// cuando no hay ningún Update.exe real al lado.
  String get _updateExePath => p.join(
        p.dirname(p.dirname(Platform.resolvedExecutable)),
        'Update.exe',
      );

  bool get canApplyUpdates => File(_updateExePath).existsSync();

  /// Descarga el asset "Full" al directorio temporal del sistema y verifica
  /// su SHA-256 contra el que publica el propio feed de Velopack antes de
  /// devolver la ruta -- nunca se le pasa a Update.exe un fichero a medias
  /// o corrupto.
  Future<String> stageUpdate(
    UpdateAsset asset, {
    void Function(int received, int total)? onProgress,
  }) async {
    final tempDir = await getTemporaryDirectory();
    final savePath = p.join(tempDir.path, asset.fileName);

    await _apiClient.request(
      (dio) => dio.download(
        '/public/client-updates/download/${asset.fileName}',
        savePath,
        onReceiveProgress: onProgress,
      ),
    );

    final digest = await sha256.bind(File(savePath).openRead()).first;
    if (digest.toString().toUpperCase() != asset.sha256.toUpperCase()) {
      await File(savePath).delete();
      throw UpdateApplyException(
        'El paquete descargado no coincide con el publicado -- se descarta, no se aplica.',
      );
    }
    return savePath;
  }

  /// Lanza `Update.exe apply` como proceso DESTACADO (no se espera a que
  /// termine) y a continuación cierra este proceso. Es necesario en ese
  /// orden: Update.exe usa `--waitPid` para esperar a que ESTE proceso
  /// termine antes de sustituir los ficheros que tiene abiertos ahora
  /// mismo -- si en vez de eso se esperara aquí con `Process.run`, ninguno
  /// de los dos avanzaría nunca (cada uno esperando al otro).
  Future<void> applyAndRestart(String packagePath) async {
    if (!canApplyUpdates) {
      throw UpdateApplyException(
        'Update.exe no está presente junto a esta instalación (¿build de desarrollo?).',
      );
    }
    await Process.start(
      _updateExePath,
      ['apply', '--package', packagePath, '--waitPid', '$pid', '--silent'],
      mode: ProcessStartMode.detached,
    );
    exit(0);
  }
}
