import 'dart:io';
import 'dart:typed_data';

import 'package:nexus_startup_task/nexus_startup_task.dart';
import 'package:win32_registry/win32_registry.dart';

const _appName = 'NexusCloud';
const _runKeyPath = r'Software\Microsoft\Windows\CurrentVersion\Run';
const _startupApprovedKeyPath =
    r'Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run';

/// Arranca NexusCloud al iniciar sesión en Windows. Hay **dos** mecanismos,
/// incompatibles entre sí, y este servicio elige el correcto en [init]:
///
///  - **Build suelto (sin empaquetar)**: escribe directamente las dos
///    claves de registro de `HKEY_CURRENT_USER` (`...\Run` y
///    `...\Explorer\StartupApproved\Run`), el mismo algoritmo y formato que
///    usa el propio Windows. Se implementó a mano (slice 10) en vez de con
///    el paquete `launch_at_startup` porque su única versión publicada fija
///    `win32_registry ^2.0.0`, incompatible con el `win32 ^6.0.1` que ya
///    exige `flutter_secure_storage`.
///
///  - **Empaquetado en MSIX**: el registro se **virtualiza** y las
///    escrituras nunca llegan al `HKCU` real (confirmado empíricamente:
///    el interruptor "parecía" funcionar pero la app no arrancaba). El
///    camino soportado por Windows es la extensión `windows.startupTask`
///    del manifiesto + la API WinRT `Windows.ApplicationModel.StartupTask`,
///    que expone el plugin interno `nexus_startup_task`.
///
/// [init] detecta el caso preguntando a la StartupTask: si responde algo
/// distinto de `unavailable`, la app está empaquetada y hay tarea declarada
/// -> se usa ese camino; si no, el registro.
class LaunchAtStartupService {
  bool _useStartupTask = false;

  Future<void> init() async {
    final state = await NexusStartupTask.getState();
    _useStartupTask = state != StartupTaskState.unavailable;
  }

  Future<bool> isEnabled() async {
    if (_useStartupTask) {
      final s = await NexusStartupTask.getState();
      return s == StartupTaskState.enabled ||
          s == StartupTaskState.enabledByPolicy;
    }
    return _registryIsEnabled();
  }

  /// Un fallo real de escritura de registro se entera por la excepción que
  /// lance; quien llame debe envolver esto en `try/catch`. En modo
  /// StartupTask, además, activar puede quedar en nada si el usuario ya
  /// había desactivado la tarea desde Configuración -> "Aplicaciones de
  /// inicio" (Windows no deja que la app lo revierta por código): en ese
  /// caso [setEnabled] `true` **lanza**, para que la UI revierta el
  /// interruptor y no mienta.
  Future<void> setEnabled(bool value) async {
    if (_useStartupTask) {
      if (value) {
        final result = await NexusStartupTask.requestEnable();
        final ok = result == StartupTaskState.enabled ||
            result == StartupTaskState.enabledByPolicy;
        if (!ok) {
          throw StateError(
            'Windows no permitió activar el arranque automático '
            '(estado: ${result.name}). Si lo desactivaste desde '
            'Configuración > Aplicaciones de inicio, actívalo desde ahí.',
          );
        }
      } else {
        await NexusStartupTask.disable();
      }
      return;
    }

    if (value) {
      await _registryEnable();
    } else {
      await _registryDisable();
    }
  }

  // --- camino registro (build suelto) ---------------------------------

  Future<bool> _registryIsEnabled() async {
    final runKey = _openRunKey();
    final String? value;
    try {
      value = runKey.getString(_appName);
    } finally {
      runKey.close();
    }
    if (value != Platform.resolvedExecutable) return false;
    return _isStartupApproved();
  }

  Future<void> _registryEnable() async {
    final runKey = _openRunKey();
    try {
      runKey.setValue(
        _appName,
        RegistryValue.string(Platform.resolvedExecutable),
      );
    } finally {
      runKey.close();
    }

    // "2" como primer byte de estos 12 significa que el autoarranque está
    // activado -- formato que usa el propio Windows para esta clave, no
    // uno inventado aquí.
    final bytes = Uint8List(12)..[0] = 2;
    final approvedKey = _openStartupApprovedKey();
    try {
      approvedKey.setValue(_appName, RegistryValue.binary(bytes));
    } finally {
      approvedKey.close();
    }
  }

  Future<void> _registryDisable() async {
    final runKey = _openRunKey();
    try {
      _removeValueIfPresent(runKey, _appName);
    } finally {
      runKey.close();
    }

    final approvedKey = _openStartupApprovedKey();
    try {
      _removeValueIfPresent(approvedKey, _appName);
    } finally {
      approvedKey.close();
    }
  }

  /// Un primer byte impar en `StartupApproved\Run` bloquea el
  /// autoarranque; ausente, vacío, o primer byte par lo permite -- mismo
  /// criterio que usa el propio Windows (y el Administrador de tareas
  /// cuando el usuario lo desactiva desde ahí).
  Future<bool> _isStartupApproved() async {
    final approvedKey = _openStartupApprovedKey();
    final Uint8List? value;
    try {
      value = approvedKey.getBinary(_appName);
    } finally {
      approvedKey.close();
    }
    if (value == null || value.isEmpty) return true;
    return value[0].isEven;
  }

  void _removeValueIfPresent(RegistryKey key, String name) {
    if (key.getValue(name) != null) {
      key.removeValue(name);
    }
  }

  // `create: true` para que abrir la clave la cree si no existiera todavía
  // (defensivo -- en la práctica ambas ya existen en cualquier Windows
  // moderno) sin arriesgar nada: `RegCreateKeyEx` sobre una clave YA
  // existente simplemente la abre, no la vacía ni la recrea, así que esto
  // nunca toca las entradas de otras apps que ya vivan en la misma clave.
  RegistryKey _openRunKey() => CURRENT_USER.open(
    _runKeyPath,
    config: const RegistryOpenConfig(access: RegistryAccess.all, create: true),
  );

  RegistryKey _openStartupApprovedKey() => CURRENT_USER.open(
    _startupApprovedKeyPath,
    config: const RegistryOpenConfig(access: RegistryAccess.all, create: true),
  );
}
