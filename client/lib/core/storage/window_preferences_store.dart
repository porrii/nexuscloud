/// Preferencias de comportamiento de la ventana (slices 9-10). NO son
/// secretas -- mismo criterio que `ServerConfigStore`, no necesita el
/// almacén seguro del SO.
abstract interface class WindowPreferencesStore {
  Future<void> saveMinimizeToTrayOnClose(bool value);

  /// `false` si no hay nada guardado todavía -- cerrar la ventana se
  /// comporta exactamente igual que antes de este slice hasta que el
  /// usuario activa el ajuste explícitamente.
  Future<bool> readMinimizeToTrayOnClose();

  /// Si arrancar ya con la ventana escondida (slice 10) -- ver
  /// `LaunchAtStartupService` sobre por qué esto NO duplica el estado de
  /// "arrancar con Windows" en sí (esa fuente de verdad es el propio
  /// registro de Windows, no `shared_preferences`).
  Future<void> saveStartMinimized(bool value);

  /// `false` si no hay nada guardado todavía.
  Future<bool> readStartMinimized();
}
