import 'package:equatable/equatable.dart';

/// Ajustes de sincronización automática (slice 8): si está activada y con
/// qué intervalo. `Equatable` no es cosmético aquí -- `readAutoSync()`
/// reconstruye una instancia nueva en cada llamada a partir de primitivos
/// sueltos de `shared_preferences`, y sin igualdad por valor un test de
/// "guardar y releer da lo mismo" no comprobaría nada real.
class AutoSyncSettings extends Equatable {
  const AutoSyncSettings({required this.enabled, required this.intervalMinutes});

  final bool enabled;
  final int intervalMinutes;

  static const disabled = AutoSyncSettings(enabled: false, intervalMinutes: 15);

  @override
  List<Object?> get props => [enabled, intervalMinutes];
}
