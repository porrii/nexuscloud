/// Preferencias de comportamiento de la ventana (slice 9). NO son secretas
/// -- mismo criterio que `ServerConfigStore`, no necesita el almacén seguro
/// del SO.
abstract interface class WindowPreferencesStore {
  Future<void> saveMinimizeToTrayOnClose(bool value);

  /// `false` si no hay nada guardado todavía -- cerrar la ventana se
  /// comporta exactamente igual que antes de este slice hasta que el
  /// usuario activa el ajuste explícitamente.
  Future<bool> readMinimizeToTrayOnClose();
}
