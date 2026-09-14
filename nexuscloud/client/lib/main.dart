import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'core/di/service_locator.dart';
import 'core/storage/window_preferences_store.dart';
import 'core/theme/app_theme.dart';
import 'core/window/app_tray_service.dart';
import 'core/window/launch_at_startup_service.dart';
import 'features/auth/presentation/pages/auth_gate_page.dart';
import 'features/sync/domain/repositories/sync_config_repository.dart';
import 'features/sync/domain/services/auto_sync_scheduler.dart';
import 'features/sync/domain/services/local_change_watcher_service.dart';
import 'features/sync/domain/services/sync_engine.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await setupServiceLocator();
  // Reactiva el auto-sync ya configurado en una sesión anterior (slice 8)
  // sin que el usuario tenga que volver a entrar en Ajustes. Seguro de
  // llamar aunque el auto-login todavía no haya terminado -- cada tick
  // comprueba la sesión antes de hacer nada (ver `AutoSyncScheduler`).
  await sl<AutoSyncScheduler>().start();
  // Umbral de la guarda anti-"borrado masivo" (#23): aplica el valor
  // persistido a la instancia de `SyncEngine` ya construida -- mismo
  // criterio que el auto-sync de arriba, sin bandeja ni segundo plano de
  // por medio, es seguro llamarlo aquí en cada arranque (ver la nota en
  // `SyncEngine._maxAutoDeleteBatch`).
  sl<SyncEngine>().updateMaxAutoDeleteBatch(
    await sl<SyncConfigRepository>().readMaxAutoDeleteBatch(),
  );
  // Vigilancia de filesystem (slice 17): mismo criterio que el auto-sync de
  // arriba -- reactiva lo que ya estuviera activado en una sesión anterior.
  await sl<LocalChangeWatcherService>().start();
  // Bandeja del sistema (slice 9): icono + comportamiento de cierre, antes
  // de que la ventana pueda recibir ningún evento (ver `AppTrayService`).
  await sl<AppTrayService>().init();
  // Arrancar con Windows (slice 10): `setup()` es síncrono y sin I/O real,
  // seguro de llamar en cada arranque (ver `LaunchAtStartupService`).
  await sl<LaunchAtStartupService>().init();

  // Leído ANTES de `runApp()` a propósito (hallazgo real de verificación,
  // no el diseño original del plan): si este `await` queda *entre*
  // `runApp()` y `addPostFrameCallback`, el hueco async that deja -- una
  // lectura de `shared_preferences` por canal de plataforma, típicamente
  // unas decenas de ms -- basta para que el primer frame ya se haya
  // renderizado (y el `Show()` nativo ya haya disparado) antes de que el
  // callback llegue a registrarse; como no hay ninguna animación que
  // dispare un segundo frame, ese callback se queda esperando y la
  // ventana nunca se esconde ("aparece y se queda", confirmado en un
  // build real: `nexuscloud_client` quedó visible de forma estable, no un
  // destello). Con la lectura resuelta antes, `runApp()` y el registro
  // del callback quedan en el mismo tramo síncrono, sin ningún `await` de
  // por medio -- ver la nota bajo `runApp()` para el resto del porqué.
  final startMinimized = await sl<WindowPreferencesStore>()
      .readStartMinimized();

  runApp(const NexusCloudApp());

  // "Iniciar minimizado" -- DESPUÉS de `runApp()`, nunca antes: la ventana
  // nativa todavía no se ha mostrado en ningún momento previo a esto (se
  // muestra recién en el primer frame que Flutter renderiza), así que un
  // `hide()` antes de `runApp()` escondería, en el mejor de los casos, una
  // ventana que ya estaba invisible -- no evitaría el show() nativo que
  // llega después. `addPostFrameCallback` compite por ese mismo primer
  // frame en vez de ejecutarse antes sin ninguna coordinación con él --
  // y para que compita de verdad (no solo en teoría) su registro tiene
  // que quedar pegado a `runApp()`, sin ningún `await` entre medio.
  //
  // Un solo `hide()` en el postFrameCallback NO basta -- comprobado con un
  // build real instrumentado con prints: `hide()` se llama y termina sin
  // ninguna excepción, pero la ventana queda visible igualmente. La causa
  // real (no solo teórica): el `Show()` nativo de `flutter_window.cpp`
  // está atado a que el frame quede *presentado* en pantalla (hilo de
  // raster), mientras que `addPostFrameCallback` se dispara al terminar de
  // *construir* ese mismo frame (hilo de UI) -- un paso anterior en el
  // pipeline. Nuestro `hide()` se despacha primero pero su mensaje de
  // canal de plataforma puede tardar en ejecutarse más que eso; si el
  // `Show()` nativo termina ejecutándose después, deshace nuestro
  // `hide()`. Comprobado que `waitUntilReadyToShow` de `window_manager`
  // (la vía "oficial" del paquete para esto) no ayuda en Windows: su
  // implementación C++ real para esta plataforma (leída directamente,
  // `windows/window_manager.cpp`) no toca la visibilidad de la ventana en
  // absoluto, solo crea un `ITaskbarList` para otra cosa -- es una vía
  // real en macOS, no aquí. La única forma 100% libre de parpadeo sería
  // tocar `win32_window.cpp`/`flutter_window.cpp` (quitar `WS_VISIBLE` y
  // la llamada a `Show()`), deliberadamente fuera de alcance de este
  // slice (ver Contexto del plan).
  //
  // Arreglo que sí es fiable sin tocar nada nativo: el `Show()` nativo se
  // dispara UNA sola vez (`SetNextFrameCallback` es de un solo uso, no se
  // repite en frames posteriores) y nada más en el runner vuelve a llamar
  // a `Show()`/`Hide()` por su cuenta. Un segundo `hide()` con retraso,
  // emitido después de que ese único disparo ya haya podido ocurrir, ya
  // no compite contra nada -- se queda escondida de verdad. 1200ms es
  // generoso frente a los ~800ms que tardó el peor caso medido en esta
  // máquina (build de depuración, sesión de verificación real).
  if (startMinimized) {
    WidgetsBinding.instance.addPostFrameCallback((_) => windowManager.hide());
    Future.delayed(
      const Duration(milliseconds: 1200),
      () => windowManager.hide(),
    );
  }
}

class NexusCloudApp extends StatelessWidget {
  const NexusCloudApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'NexusCloud',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      home: const AuthGatePage(),
    );
  }
}
