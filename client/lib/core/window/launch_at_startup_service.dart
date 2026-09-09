import 'dart:io';
import 'dart:typed_data';

import 'package:win32_registry/win32_registry.dart';

const _appName = 'NexusCloud';
const _runKeyPath = r'Software\Microsoft\Windows\CurrentVersion\Run';
const _startupApprovedKeyPath =
    r'Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run';

/// Arranca NexusCloud con Windows (slice 10), escribiendo directamente en
/// el registro -- NO usa el paquete `launch_at_startup` como preveía el
/// plan original de este slice. Motivo real, no una preferencia de
/// estilo: su única versión publicada (0.5.1, de hace más de un año) fija
/// `win32_registry ^2.0.0`, incompatible con el `win32 ^6.0.1` que ya
/// exige `flutter_secure_storage` (slice 1) por su propia cadena de
/// dependencias. Forzar `win32_registry: ^3.0.0` con `dependency_overrides`
/// resuelve el conflicto de *versiones* pero no el problema real:
/// `win32_registry` 3.x rediseñó por completo la API de `RegistryKey`
/// (`createValue`→`setValue`, `getBinaryValue`→`getBinary`,
/// `deleteValue`→`removeValue`, entre otros) y el propio código de
/// `launch_at_startup` 0.5.1 -- escrito contra la API 2.x -- deja de
/// compilar. Esto NO lo detectó `flutter analyze` (dio "sin problemas"),
/// solo aparece con `flutter test`/`flutter build` reales -- lección para
/// cualquier slice futuro que fuerce una versión con `dependency_overrides`:
/// no basta con que la resolución de dependencias tenga éxito, hay que
/// comprobar que compila de verdad.
///
/// El algoritmo en sí (dos claves de registro, mismo formato del valor
/// binario de `StartupApproved\Run`) es exactamente el que implementa
/// `launch_at_startup` para Windows -- confirmado leyendo su código
/// fuente real antes de escribir esto, no reinventado. Solo cambia qué
/// versión de `win32_registry` lo ejecuta.
///
/// La fuente de verdad de "¿está activado?" es el propio registro de
/// Windows (releído en cada llamada a [isEnabled]), no un booleano
/// duplicado en `shared_preferences` -- así no puede divergir si el
/// usuario lo desactiva desde fuera de esta app (p.ej. desde el
/// Administrador de tareas → "Aplicaciones de inicio", que solo toca
/// [_startupApprovedKeyPath], nunca [_runKeyPath]).
class LaunchAtStartupService {
  /// Nada que hacer de verdad -- `Future<void>` solo por consistencia de
  /// forma con `AppTrayService.init()`, no porque haga falta esperar
  /// ningún I/O real aquí.
  Future<void> init() async {}

  Future<bool> isEnabled() async {
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

  /// El booleano que devolvería un `enable()`/`disable()` al estilo
  /// `launch_at_startup` no sería fiable de todas formas (confirmado en
  /// su código fuente: siempre `true` en Windows sin MSIX, incluso si
  /// algo fuera mal) -- por eso esto es `Future<void>`. Un fallo real de
  /// escritura de registro se entera por la excepción que lance, no por
  /// ningún valor de retorno; quien llame debe envolver esto en
  /// `try/catch`, no confiar en que "no lanzó" implique éxito silencioso.
  Future<void> setEnabled(bool value) async {
    if (value) {
      await _enable();
    } else {
      await _disable();
    }
  }

  Future<void> _enable() async {
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

  Future<void> _disable() async {
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
