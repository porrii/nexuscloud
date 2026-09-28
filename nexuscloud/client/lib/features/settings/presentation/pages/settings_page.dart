import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/storage/server_config_store.dart';
import '../../../../core/storage/window_preferences_store.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/window/app_tray_service.dart';
import '../../../../core/window/launch_at_startup_service.dart';
import '../../../account/data/quota_service.dart';
import '../../../auth/domain/repositories/auth_repository.dart';
import '../../../shell/presentation/widgets/sidebar.dart' show UserAvatar;
import '../../../sync/domain/entities/auto_sync_settings.dart';
import '../../../sync/domain/repositories/sync_config_repository.dart';
import '../../../sync/domain/services/auto_sync_scheduler.dart';
import '../../../sync/domain/services/local_change_watcher_service.dart';
import '../../../sync/domain/services/sync_engine.dart';
import '../../../update/presentation/pages/updates_page.dart';

/// Intervalos de sincronización automática ofrecidos en el desplegable.
/// 5 minutos es el mínimo -- por debajo de eso, el coste de recorrer el
/// árbol remoto entero en cada tick (ADR-011, sin estado incremental) deja
/// de ser razonable para carpetas con muchos archivos.
const _autoSyncIntervalOptions = [5, 15, 30, 60];

/// Opciones ofrecidas para el umbral de la guarda anti-"borrado masivo"
/// (#23, ADR-013) -- mismo criterio que [_autoSyncIntervalOptions]: un
/// puñado de valores fijos en vez de un campo de texto libre, para no tener
/// que validar un número arbitrario aquí.
const _maxAutoDeleteBatchOptions = [5, 10, 20, 50, 100];

/// Ajustes de la app. Reúne lo que antes vivía mezclado al final de la
/// página de Sincronización (auto-sync, vigilancia, bandeja, arranque con
/// Windows) más la cuenta, el almacenamiento y las actualizaciones, que
/// tenían su propio icono suelto en la barra superior.
class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key, this.quota, this.onUpdateStateChanged});

  final StorageQuota? quota;
  final ValueChanged<bool>? onUpdateStateChanged;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();
  final SyncEngine _syncEngine = sl<SyncEngine>();
  final AutoSyncScheduler _autoSyncScheduler = sl<AutoSyncScheduler>();
  final LocalChangeWatcherService _watcherService =
      sl<LocalChangeWatcherService>();
  final AppTrayService _trayService = sl<AppTrayService>();
  final LaunchAtStartupService _launchAtStartupService =
      sl<LaunchAtStartupService>();
  // Directo a `WindowPreferencesStore`, no a través de `AppTrayService` ni
  // `LaunchAtStartupService`: "iniciar minimizado" no es dueño de ninguno
  // de los dos (ver el plan del slice 10 sobre por qué la coordinación
  // entre ambos vive aquí, en la página, y no dentro de un servicio).
  final WindowPreferencesStore _windowPreferencesStore =
      sl<WindowPreferencesStore>();

  bool _autoSyncEnabled = false;
  int _autoSyncIntervalMinutes = _autoSyncIntervalOptions[1];
  int _maxAutoDeleteBatch = 10;
  bool _watchLocalChangesEnabled = false;

  /// Instantánea de `AppTrayService.minimizeToTrayOnClose` (slice 9): el
  /// servicio ya lo cachea tras su propio `init()`, así que leerlo aquí no
  /// necesita otro `await`.
  bool _minimizeToTrayOnClose = false;

  /// A diferencia de `_minimizeToTrayOnClose`, no hay valor cacheado
  /// síncrono disponible -- `launchAtStartup.isEnabled()` relee el
  /// registro de Windows de verdad en cada llamada (es la fuente de
  /// verdad, no `shared_preferences`), así que empieza en `false` hasta
  /// que `_loadLaunchAtStartupSettings` resuelve.
  bool _launchAtStartupEnabled = false;
  bool _startMinimized = false;
  String? _launchAtStartupError;
  String? _serverUrl;

  @override
  void initState() {
    super.initState();
    _minimizeToTrayOnClose = _trayService.minimizeToTrayOnClose;
    _loadAutoSyncSettings();
    _loadMaxAutoDeleteBatch();
    _loadWatchLocalChangesSetting();
    _loadLaunchAtStartupSettings();
    _loadServerUrl();
  }

  Future<void> _loadAutoSyncSettings() async {
    final settings = await _configRepository.readAutoSync();
    if (!mounted) return;
    setState(() {
      _autoSyncEnabled = settings.enabled;
      _autoSyncIntervalMinutes = settings.intervalMinutes;
    });
  }

  Future<void> _loadMaxAutoDeleteBatch() async {
    final value = await _configRepository.readMaxAutoDeleteBatch();
    if (!mounted) return;
    setState(() => _maxAutoDeleteBatch = value);
  }

  Future<void> _loadWatchLocalChangesSetting() async {
    final enabled = await _configRepository.readWatchLocalChanges();
    if (!mounted) return;
    setState(() => _watchLocalChangesEnabled = enabled);
  }

  Future<void> _loadServerUrl() async {
    if (!sl.isRegistered<ServerConfigStore>()) return;
    final url = await sl<ServerConfigStore>().read();
    if (mounted) setState(() => _serverUrl = url);
  }

  /// El invariante de "iniciar minimizado no puede quedar huérfano" se
  /// cierra en DOS sitios (ver el plan del slice 10): aquí, al cargar la
  /// página, y en `_setLaunchAtStartupEnabled` al desactivar el interruptor
  /// a mano. Hace falta aquí también porque `isEnabled()` puede volverse
  /// `false` sin que esta app se entere -- p.ej. si el usuario lo
  /// desactiva desde el Administrador de tareas → "Aplicaciones de
  /// inicio", que no pasa por ningún código de esta app en absoluto.
  Future<void> _loadLaunchAtStartupSettings() async {
    final enabled = await _launchAtStartupService.isEnabled();
    final startMinimized = await _windowPreferencesStore.readStartMinimized();
    if (!enabled && startMinimized) {
      await _windowPreferencesStore.saveStartMinimized(false);
    }
    if (!mounted) return;
    setState(() {
      _launchAtStartupEnabled = enabled;
      _startMinimized = enabled && startMinimized;
    });
  }

  Future<void> _setAutoSyncEnabled(bool enabled) async {
    setState(() => _autoSyncEnabled = enabled);
    await _autoSyncScheduler.updateSettings(
      AutoSyncSettings(
        enabled: enabled,
        intervalMinutes: _autoSyncIntervalMinutes,
      ),
    );
  }

  Future<void> _setAutoSyncInterval(int minutes) async {
    setState(() => _autoSyncIntervalMinutes = minutes);
    await _autoSyncScheduler.updateSettings(
      AutoSyncSettings(enabled: _autoSyncEnabled, intervalMinutes: minutes),
    );
  }

  Future<void> _setMaxAutoDeleteBatch(int value) async {
    setState(() => _maxAutoDeleteBatch = value);
    await _configRepository.saveMaxAutoDeleteBatch(value);
    _syncEngine.updateMaxAutoDeleteBatch(value);
  }

  Future<void> _setWatchLocalChangesEnabled(bool value) async {
    setState(() => _watchLocalChangesEnabled = value);
    await _watcherService.setEnabled(value);
  }

  Future<void> _setMinimizeToTrayOnClose(bool value) async {
    setState(() => _minimizeToTrayOnClose = value);
    await _trayService.updateMinimizeToTrayOnClose(value);
  }

  /// A diferencia de los demás interruptores de esta página, envuelve la
  /// llamada en `try/catch` (ver el plan del slice 10): a diferencia de
  /// `shared_preferences`/el scheduler en memoria que respaldan los otros,
  /// esto son dos escrituras de registro Win32 no atómicas vía FFI cruda,
  /// que sí pueden lanzar -- y el booleano que devolvería `enable()`/
  /// `disable()` no serviría para detectar un fallo aunque se comprobara
  /// (siempre `true` en Windows sin MSIX, confirmado en su código fuente).
  Future<void> _setLaunchAtStartupEnabled(bool value) async {
    final previous = _launchAtStartupEnabled;
    setState(() {
      _launchAtStartupEnabled = value;
      _launchAtStartupError = null;
      // Mismo invariante que en la carga: si se desactiva, "iniciar
      // minimizado" no puede quedar activado sin que el interruptor que
      // lo controla siga visible.
      if (!value) _startMinimized = false;
    });
    try {
      await _launchAtStartupService.setEnabled(value);
      if (!value) {
        await _windowPreferencesStore.saveStartMinimized(false);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _launchAtStartupEnabled = previous;
        _launchAtStartupError =
            'No se pudo cambiar el ajuste de arranque con Windows.';
      });
    }
  }

  Future<void> _setStartMinimized(bool value) async {
    setState(() => _startMinimized = value);
    await _windowPreferencesStore.saveStartMinimized(value);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PageHeader(
            title: 'Ajustes',
            subtitle: 'Sincronización automática, comportamiento de la ventana, cuenta y actualizaciones.',
          ),
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 28),
              child: Align(
                alignment: Alignment.topLeft,
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 760),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      _buildSyncCard(context),
                      const SizedBox(height: 16),
                      _buildWindowCard(context),
                      const SizedBox(height: 16),
                      _buildAccountCard(context),
                      const SizedBox(height: 16),
                      SectionCard(
                        title: 'Actualizaciones',
                        icon: Icons.system_update_alt_rounded,
                        child: UpdatesPanel(
                          onAvailabilityChanged: widget.onUpdateStateChanged,
                        ),
                      ),
                      const SizedBox(height: 16),
                      const _ShortcutsCard(),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSyncCard(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return SectionCard(
      title: 'Sincronización automática',
      icon: Icons.sync_rounded,
      description: 'Se aplica a todas las carpetas vinculadas mientras la app esté abierta.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SettingSwitchRow(
            title: 'Sincronizar automáticamente',
            description: 'Repite la sincronización de todas las carpetas sola cada cierto tiempo.',
            value: _autoSyncEnabled,
            onChanged: _setAutoSyncEnabled,
          ),
          if (_autoSyncEnabled)
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 0, 4, 8),
              child: Row(
                children: [
                  Text('Cada', style: text.bodyMedium),
                  const SizedBox(width: 12),
                  _Dropdown<int>(
                    value: _autoSyncIntervalMinutes,
                    options: {
                      for (final m in _autoSyncIntervalOptions) m: '$m minutos',
                    },
                    onChanged: _setAutoSyncInterval,
                  ),
                ],
              ),
            ),
          const Divider(height: 16),
          SettingSwitchRow(
            title: 'Vigilar cambios locales',
            description:
                'Sincroniza poco después de guardar o borrar un archivo en una carpeta '
                'con sentido «Subir» o «Ambos». Complementa al reloj y al botón manual.',
            value: _watchLocalChangesEnabled,
            onChanged: _setWatchLocalChangesEnabled,
          ),
          const Divider(height: 16),
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 4),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Pedir confirmación antes de borrar muchos archivos',
                        style: text.bodyLarge?.copyWith(
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        'Si una sincronización (manual o automática) va a borrar más de esta '
                        'cantidad de golpe, se detiene y te enseña la lista antes.',
                        style: text.bodySmall,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 16),
                _Dropdown<int>(
                  value: _maxAutoDeleteBatch,
                  options: {
                    for (final n in _maxAutoDeleteBatchOptions) n: 'Más de $n',
                  },
                  onChanged: _setMaxAutoDeleteBatch,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildWindowCard(BuildContext context) {
    return SectionCard(
      title: 'Ventana y arranque',
      icon: Icons.desktop_windows_outlined,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SettingSwitchRow(
            title: 'Minimizar a la bandeja al cerrar',
            description:
                'La X esconde la ventana en vez de cerrar la app, así la sincronización '
                'automática sigue corriendo. El icono de la bandeja permite volver a abrirla o salir.',
            value: _minimizeToTrayOnClose,
            onChanged: _setMinimizeToTrayOnClose,
          ),
          const Divider(height: 16),
          SettingSwitchRow(
            title: 'Arrancar NexusCloud con Windows',
            description:
                'Se abre sola al iniciar sesión en Windows, para que la sincronización '
                'automática siga activa tras reiniciar el equipo.',
            value: _launchAtStartupEnabled,
            onChanged: _setLaunchAtStartupEnabled,
          ),
          if (_launchAtStartupError != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 4, 4, 8),
              child: Text(
                _launchAtStartupError!,
                style: TextStyle(color: context.palette.danger),
              ),
            ),
          if (_launchAtStartupEnabled)
            Padding(
              padding: const EdgeInsets.only(left: 20),
              child: SettingSwitchRow(
                title: 'Iniciar minimizado en la bandeja',
                description: 'Al arrancar con Windows, empieza escondida en la bandeja del sistema.',
                value: _startMinimized,
                onChanged: _setStartMinimized,
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildAccountCard(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final auth = sl<AuthRepository>();
    final user = auth.currentUser;
    final quota = widget.quota;
    final name = user == null
        ? ''
        : (user.displayName.trim().isEmpty ? user.username : user.displayName);

    return SectionCard(
      title: 'Cuenta',
      icon: Icons.account_circle_outlined,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              UserAvatar(name: name, size: 44),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(name, style: text.titleMedium),
                    Text(
                      [
                        if (user != null) '@${user.username}',
                        if (user?.email != null) user!.email!,
                      ].join(' · '),
                      style: text.bodySmall,
                    ),
                    if (_serverUrl != null) ...[
                      const SizedBox(height: 4),
                      Row(
                        children: [
                          Icon(
                            Icons.dns_outlined,
                            size: 14,
                            color: p.textMuted,
                          ),
                          const SizedBox(width: 6),
                          Flexible(
                            child: Text(
                              _serverUrl!,
                              style: text.bodySmall,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: () => auth.logout(),
                icon: const Icon(Icons.logout_rounded, size: 18),
                label: const Text('Cerrar sesión'),
              ),
            ],
          ),
          if (quota != null) ...[
            const SizedBox(height: 18),
            _QuotaBreakdown(quota: quota),
          ],
        ],
      ),
    );
  }
}

class _QuotaBreakdown extends StatelessWidget {
  const _QuotaBreakdown({required this.quota});

  final StorageQuota quota;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final limit = quota.limitBytes;
    final total = (limit ?? quota.usedBytes).clamp(1, 1 << 62);
    final segments = [
      ('Archivos', quota.filesBytes, p.accent),
      ('Versiones', quota.versionsBytes, const Color(0xFF8B5CF6)),
      ('Papelera', quota.trashBytes, p.warning),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Text(
              'Almacenamiento',
              style: text.bodyLarge?.copyWith(fontWeight: FontWeight.w500),
            ),
            const Spacer(),
            Text(
              limit == null
                  ? '${formatBytes(quota.usedBytes)} usados · sin límite'
                  : '${formatBytes(quota.usedBytes)} de ${formatBytes(limit)}',
              style: text.bodySmall,
            ),
          ],
        ),
        const SizedBox(height: 10),
        ClipRRect(
          borderRadius: BorderRadius.circular(6),
          child: SizedBox(
            height: 10,
            child: Row(
              children: [
                for (final (_, bytes, color) in segments)
                  if (bytes > 0)
                    Expanded(
                      flex: ((bytes / total) * 1000).round().clamp(1, 1000),
                      child: ColoredBox(color: color),
                    ),
                if (limit != null && quota.usedBytes < limit)
                  Expanded(
                    flex: (((limit - quota.usedBytes) / total) * 1000)
                        .round()
                        .clamp(1, 1000),
                    child: ColoredBox(color: p.surfaceMuted),
                  ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 10),
        Wrap(
          spacing: 18,
          runSpacing: 6,
          children: [
            for (final (label, bytes, color) in segments)
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Container(
                    width: 9,
                    height: 9,
                    decoration: BoxDecoration(
                      color: color,
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text('$label · ${formatBytes(bytes)}', style: text.bodySmall),
                ],
              ),
          ],
        ),
      ],
    );
  }
}

class _Dropdown<T> extends StatelessWidget {
  const _Dropdown({
    required this.value,
    required this.options,
    required this.onChanged,
  });

  final T value;
  final Map<T, String> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: p.borderStrong),
      ),
      child: DropdownButtonHideUnderline(
        child: DropdownButton<T>(
          value: value,
          borderRadius: BorderRadius.circular(10),
          dropdownColor: p.surface,
          style: Theme.of(context).textTheme.bodyMedium,
          items: [
            for (final entry in options.entries)
              DropdownMenuItem(value: entry.key, child: Text(entry.value)),
          ],
          onChanged: (v) {
            if (v != null) onChanged(v);
          },
        ),
      ),
    );
  }
}

class _ShortcutsCard extends StatelessWidget {
  const _ShortcutsCard();

  static const _shortcuts = [
    ('Ctrl+K', 'Buscar en todas tus carpetas'),
    ('Ctrl+1 … 5', 'Ir a cada sección de la barra lateral'),
    ('Ctrl+U', 'Subir archivos a la carpeta actual'),
    ('Ctrl+N', 'Nueva carpeta'),
    ('Ctrl+F', 'Filtrar la carpeta actual'),
    ('F2', 'Renombrar'),
    ('Supr', 'Mover a la papelera'),
    ('Ctrl+A', 'Seleccionar todo'),
    ('Retroceso · Alt+↑', 'Subir un nivel'),
    ('F5', 'Actualizar'),
    ('Mayús+F10', 'Menú contextual del elemento'),
  ];

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return SectionCard(
      title: 'Atajos de teclado',
      icon: Icons.keyboard_outlined,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final columns = constraints.maxWidth > 560 ? 2 : 1;
          final width = (constraints.maxWidth - (columns - 1) * 24) / columns;
          return Wrap(
            spacing: 24,
            runSpacing: 10,
            children: [
              for (final (keys, action) in _shortcuts)
                SizedBox(
                  width: width,
                  child: Row(
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 3,
                        ),
                        decoration: BoxDecoration(
                          color: p.surfaceMuted,
                          borderRadius: BorderRadius.circular(6),
                          border: Border.all(color: p.border),
                        ),
                        child: Text(
                          keys,
                          style: TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                            color: p.textSecondary,
                          ),
                        ),
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          action,
                          style: text.bodyMedium?.copyWith(fontSize: 13.5),
                        ),
                      ),
                    ],
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}
