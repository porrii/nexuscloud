import 'package:flutter/services.dart';

/// Estado de una StartupTask de MSIX, reflejo 1:1 de
/// `Windows.ApplicationModel.StartupTaskState`.
enum StartupTaskState {
  /// Registrada pero desactivada; la app puede pedir activarla.
  disabled,

  /// El usuario la desactivó (Administrador de tareas / Configuración).
  /// La app NO puede volver a activarla por código: hay que hacerlo a mano.
  disabledByUser,

  /// Desactivada por política de grupo/empresa. Inamovible.
  disabledByPolicy,

  /// Activada.
  enabled,

  /// Activada por política. Inamovible.
  enabledByPolicy,

  /// No disponible (no empaquetado, tarea no encontrada, error).
  unavailable,
}

StartupTaskState _parse(String? s) {
  switch (s) {
    case 'disabled':
      return StartupTaskState.disabled;
    case 'disabledByUser':
      return StartupTaskState.disabledByUser;
    case 'disabledByPolicy':
      return StartupTaskState.disabledByPolicy;
    case 'enabled':
      return StartupTaskState.enabled;
    case 'enabledByPolicy':
      return StartupTaskState.enabledByPolicy;
    default:
      return StartupTaskState.unavailable;
  }
}

/// Acceso a la StartupTask declarada en el manifiesto MSIX. Todos los
/// métodos devuelven `unavailable` en vez de lanzar cuando la app no está
/// empaquetada o la tarea no existe -- quien llama decide qué hacer.
class NexusStartupTask {
  static const _channel = MethodChannel('nexuscloud/startup_task');

  /// Estado actual de la StartupTask.
  static Future<StartupTaskState> getState() async {
    try {
      final s = await _channel.invokeMethod<String>('getState');
      return _parse(s);
    } on PlatformException {
      return StartupTaskState.unavailable;
    } on MissingPluginException {
      return StartupTaskState.unavailable;
    }
  }

  /// Pide activar la StartupTask. Windows puede mostrar un diálogo la
  /// primera vez. Devuelve el estado REAL resultante: si el usuario ya la
  /// había desactivado a mano, seguirá en `disabledByUser`.
  static Future<StartupTaskState> requestEnable() async {
    try {
      final s = await _channel.invokeMethod<String>('requestEnable');
      return _parse(s);
    } on PlatformException {
      return StartupTaskState.unavailable;
    } on MissingPluginException {
      return StartupTaskState.unavailable;
    }
  }

  /// Desactiva la StartupTask. No falla si ya estaba desactivada.
  static Future<void> disable() async {
    try {
      await _channel.invokeMethod<void>('disable');
    } on PlatformException {
      // best-effort
    } on MissingPluginException {
      // sin plugin (no empaquetado): nada que hacer
    }
  }
}
