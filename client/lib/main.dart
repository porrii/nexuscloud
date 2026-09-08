import 'package:flutter/material.dart';

import 'core/di/service_locator.dart';
import 'core/theme/app_theme.dart';
import 'core/window/app_tray_service.dart';
import 'features/auth/presentation/pages/auth_gate_page.dart';
import 'features/sync/domain/services/auto_sync_scheduler.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await setupServiceLocator();
  // Reactiva el auto-sync ya configurado en una sesión anterior (slice 8)
  // sin que el usuario tenga que volver a entrar en Ajustes. Seguro de
  // llamar aunque el auto-login todavía no haya terminado -- cada tick
  // comprueba la sesión antes de hacer nada (ver `AutoSyncScheduler`).
  await sl<AutoSyncScheduler>().start();
  // Bandeja del sistema (slice 9): icono + comportamiento de cierre, antes
  // de que la ventana pueda recibir ningún evento (ver `AppTrayService`).
  await sl<AppTrayService>().init();
  runApp(const NexusCloudApp());
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
