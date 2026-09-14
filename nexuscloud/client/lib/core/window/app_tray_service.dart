import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';

import '../storage/window_preferences_store.dart';

const _showMenuItemKey = 'show';
const _exitMenuItemKey = 'exit';

/// Bandeja del sistema + "minimizar en vez de cerrar" (slice 9). Servicio de
/// vida larga, un único singleton en DI -- misma forma que
/// `SyncEngine`/`AutoSyncScheduler`: sin entidad de dominio propia (solo un
/// booleano persistido), así que vive en `core/window/` en vez de en una
/// feature nueva -- ver el razonamiento completo en el plan de este slice.
///
/// El icono de bandeja se muestra siempre que el proceso esté corriendo,
/// sin importar el valor de [minimizeToTrayOnClose] -- es la única señal
/// visible de que la app sigue viva una vez escondida, y "Salir" es útil
/// incluso con el ajuste apagado. Solo el comportamiento de [onWindowClose]
/// depende del ajuste.
///
/// IMPORTANTE, confirmado leyendo el código fuente C++ real de
/// `window_manager` (no solo su documentación): el runner nativo de Windows
/// (`windows/runner/main.cpp`, `SetQuitOnClose(true)`) NO se toca para este
/// slice. Cuando `setPreventClose(true)` está activo, `WM_CLOSE` se
/// intercepta dentro del propio plugin antes de que ese flag nativo entre
/// en juego; cuando está desactivado (por defecto), cerrar con la X sigue
/// su camino nativo normal sin ningún cambio de comportamiento. Tocar
/// `main.cpp` dejaría el proceso corriendo como zombi invisible en cuanto
/// el ajuste estuviera apagado -- ver el plan de este slice para el
/// análisis completo.
class AppTrayService with TrayListener, WindowListener {
  AppTrayService({required WindowPreferencesStore preferencesStore})
      : _preferencesStore = preferencesStore;

  final WindowPreferencesStore _preferencesStore;

  bool _minimizeToTrayOnClose = false;

  /// Valor en caché tras [init] -- para que la UI pueda leer el estado
  /// inicial del interruptor sin otro `await` en su propio `initState`.
  bool get minimizeToTrayOnClose => _minimizeToTrayOnClose;

  Future<void> init() async {
    await windowManager.ensureInitialized();

    // Los listeners se registran ANTES de aplicar `setPreventClose` -- si
    // fuera al revés, habría una ventana (pequeña pero real) en la que el
    // cierre ya estuviera interceptado a nivel nativo sin que el lado Dart
    // todavía supiera reaccionar a `onWindowClose`.
    windowManager.addListener(this);
    trayManager.addListener(this);

    _minimizeToTrayOnClose =
        await _preferencesStore.readMinimizeToTrayOnClose();
    await windowManager.setPreventClose(_minimizeToTrayOnClose);

    await trayManager.setIcon('windows/runner/resources/app_icon.ico');
    await trayManager.setContextMenu(
      Menu(
        items: [
          MenuItem(key: _showMenuItemKey, label: 'Mostrar NexusCloud'),
          MenuItem.separator(),
          MenuItem(key: _exitMenuItemKey, label: 'Salir'),
        ],
      ),
    );
  }

  Future<void> updateMinimizeToTrayOnClose(bool value) async {
    _minimizeToTrayOnClose = value;
    await _preferencesStore.saveMinimizeToTrayOnClose(value);
    await windowManager.setPreventClose(value);
  }

  @override
  void onWindowClose() {
    // No interceptado en absoluto cuando el ajuste está apagado (ver el
    // comentario de clase) -- este método solo llega a ejecutarse de
    // verdad cuando `setPreventClose(true)` está activo, así que aquí
    // siempre corresponde esconder, nunca "dejar cerrar".
    windowManager.hide();
  }

  @override
  void onTrayIconMouseDown() {
    windowManager.show();
    windowManager.focus();
  }

  @override
  void onTrayIconRightMouseDown() {
    // Obligatorio: `tray_manager` no despliega su menú contextual por sí
    // solo con el clic derecho -- confirmado en el código fuente del
    // plugin. Sin esto, el menú de bandeja no aparece nunca.
    trayManager.popUpContextMenu();
  }

  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    switch (menuItem.key) {
      case _showMenuItemKey:
        windowManager.show();
        windowManager.focus();
      case _exitMenuItemKey:
        // `trayManager.destroy()` primero: `windowManager.destroy()` llama
        // a `PostQuitMessage` directamente sin pasar por `WM_DESTROY`, que
        // es donde el plugin de bandeja limpiaría el icono automáticamente
        // -- sin este paso, el icono queda "fantasma" hasta que Windows lo
        // nota por su cuenta. `windowManager.destroy()` fuerza el cierre
        // real sin pasar por `setPreventClose` en ningún momento, así que
        // "Salir" funciona igual con el ajuste activado o no.
        trayManager.destroy();
        windowManager.destroy();
    }
  }

  void dispose() {
    windowManager.removeListener(this);
    trayManager.removeListener(this);
  }
}
