import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/di/service_locator.dart';
import '../../../core/transfers/transfer_panel.dart';
import '../../../core/transfers/transfer_queue.dart';
import '../../account/data/quota_service.dart';
import '../../admin/data/admin_service.dart';
import '../../admin/presentation/pages/admin_page.dart';
import '../../auth/domain/repositories/auth_repository.dart';
import '../../files/presentation/file_browser_controller.dart';
import '../../files/presentation/pages/file_browser_page.dart';
import '../../files/presentation/pages/trash_hub_page.dart';
import '../../search/presentation/pages/search_page.dart';
import '../../settings/presentation/pages/settings_page.dart';
import '../../sharing/presentation/pages/shared_hub_page.dart';
import '../../sync/presentation/pages/sync_settings_page.dart';
import '../../sync/presentation/sync_activity.dart';
import '../../update/data/update_check_service.dart';
import '../../update/domain/entities/update_check_result.dart';
import 'widgets/global_shortcuts.dart';
import 'widgets/sidebar.dart';

/// Estructura principal tras iniciar sesión: barra lateral fija con las
/// secciones y el área de contenido a la derecha, con el panel de
/// transferencias abajo. Sustituye a la antigua `AppBar` con nueve iconos
/// que empujaban páginas una encima de otra.
///
/// Cada sección se construye la primera vez que se visita y se conserva
/// después (una subida sigue su curso aunque cambies de sección, y al
/// volver al explorador sigues en la misma carpeta).
class AppShell extends StatefulWidget {
  const AppShell({super.key});

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  final AuthRepository _authRepository = sl<AuthRepository>();
  final TransferQueue _transfers = sl<TransferQueue>();
  final SyncActivity _syncActivity = sl<SyncActivity>();
  final _browserController = FileBrowserController();
  final _searchController = TextEditingController();
  final _searchFocus = FocusNode(debugLabel: 'global-search');

  /// Todo el shell, para saber si el foco sigue dentro de él.
  final _shellFocus = FocusNode(debugLabel: 'shell', skipTraversal: true);

  /// Área de contenido: recibe el foco al cambiar a una sección que no se
  /// lo da a sí misma, para que Tab y sus atajos funcionen sin hacer clic.
  final _contentFocus = FocusNode(
    debugLabel: 'shell-content',
    skipTraversal: true,
  );

  ShellSection _section = ShellSection.files;
  final Set<ShellSection> _visited = {ShellSection.files};
  String _searchQuery = '';
  int _searchGeneration = 0;
  StorageQuota? _quota;
  bool _updateAvailable = false;
  bool _isAdmin = false;

  /// El servidor ya respondió (sí o no). Mientras sea `false`, se reintenta
  /// en cada cambio de sección: un fallo de red al arrancar no debe ocultar
  /// «Administración» hasta el siguiente inicio de sesión.
  bool _adminChecked = false;
  int _lastActiveTransfers = 0;

  @override
  void initState() {
    super.initState();
    _syncActivity.init();
    _transfers.addListener(_onTransfersChanged);
    _refreshQuota();
    _checkForUpdatesSilently();
    _checkAdmin();
  }

  Future<void> _checkAdmin() async {
    if (_adminChecked || !sl.isRegistered<AdminService>()) return;
    final isAdmin = await sl<AdminService>().isAdmin();
    if (!mounted || isAdmin == null) return;
    _adminChecked = true;
    if (isAdmin) setState(() => _isAdmin = true);
  }

  @override
  void dispose() {
    _transfers.removeListener(_onTransfersChanged);
    _searchController.dispose();
    _searchFocus.dispose();
    _shellFocus.dispose();
    _contentFocus.dispose();
    super.dispose();
  }

  /// Al terminar un lote de transferencias cambia el espacio usado.
  void _onTransfersChanged() {
    final active = _transfers.activeCount;
    if (_lastActiveTransfers > 0 && active == 0) _refreshQuota();
    _lastActiveTransfers = active;
  }

  Future<void> _refreshQuota() async {
    final quota = await sl<QuotaService>().fetch();
    if (mounted) setState(() => _quota = quota);
  }

  /// Silenciosa a propósito: solo enciende el aviso de "Nueva" en Ajustes
  /// si hay algo nuevo, nunca descarga ni muestra un error si la
  /// comprobación falla (es una capacidad opcional del servidor).
  Future<void> _checkForUpdatesSilently() async {
    final result = await sl<UpdateCheckService>().check();
    if (!mounted) return;
    if (result.status == UpdateCheckStatus.updateAvailable) {
      setState(() => _updateAvailable = true);
    }
  }

  void _select(ShellSection section) {
    setState(() {
      _section = section;
      _visited.add(section);
    });
    if (section == ShellSection.settings || section == ShellSection.trash) {
      _refreshQuota();
    }
    _keepFocusInside();
    _checkAdmin();
  }

  /// La sección que se oculta pierde el foco (ExcludeFocus) y este sube al
  /// FocusScope de la ruta, fuera del shell. Se comprueba en un evento
  /// posterior -- cuando ya se han aplicado ese cambio y el que pida la
  /// propia sección nueva (el explorador enfoca su lista) -- y, si el foco
  /// quedó fuera, se trae al área de contenido.
  void _keepFocusInside() {
    Future<void>(() {
      if (!mounted || _shellFocus.hasFocus) return;
      _contentFocus.requestFocus();
    });
  }

  void _search(String query) {
    final trimmed = query.trim();
    if (trimmed.isEmpty) return;
    setState(() {
      _searchQuery = trimmed;
      _searchGeneration++;
      _section = ShellSection.search;
      _visited.add(ShellSection.search);
    });
    _keepFocusInside();
  }

  void _openLocation(String path, String? selectName) {
    _select(ShellSection.files);
    _browserController.open(path, selectName: selectName);
  }

  Widget _buildSection(ShellSection section) {
    return switch (section) {
      ShellSection.files => FileBrowserPage(
        controller: _browserController,
        transferQueue: _transfers,
      ),
      ShellSection.shared => SharedHubPage(transferQueue: _transfers),
      ShellSection.sync => const SyncSettingsPage(),
      ShellSection.trash => const TrashHubPage(),
      ShellSection.settings => SettingsPage(
        quota: _quota,
        onUpdateStateChanged: (available) =>
            setState(() => _updateAvailable = available),
      ),
      ShellSection.admin => AdminPage(
        currentUserId: _authRepository.currentUser?.id,
      ),
      ShellSection.search => SearchPage(
        key: ValueKey('search-$_searchGeneration'),
        initialQuery: _searchQuery,
        onOpenLocation: _openLocation,
      ),
    };
  }

  @override
  Widget build(BuildContext context) {
    final sections = ShellSection.values;
    return GlobalShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.digit1, control: true): () =>
            _select(ShellSection.files),
        const SingleActivator(LogicalKeyboardKey.digit2, control: true): () =>
            _select(ShellSection.shared),
        const SingleActivator(LogicalKeyboardKey.digit3, control: true): () =>
            _select(ShellSection.sync),
        const SingleActivator(LogicalKeyboardKey.digit4, control: true): () =>
            _select(ShellSection.trash),
        const SingleActivator(LogicalKeyboardKey.digit5, control: true): () =>
            _select(ShellSection.settings),
        const SingleActivator(LogicalKeyboardKey.digit6, control: true): () {
          if (_isAdmin) _select(ShellSection.admin);
        },
        const SingleActivator(LogicalKeyboardKey.keyK, control: true): () {
          _searchFocus.requestFocus();
          _searchController.selection = TextSelection(
            baseOffset: 0,
            extentOffset: _searchController.text.length,
          );
        },
      },
      child: Focus(
        focusNode: _shellFocus,
        child: Scaffold(
          body: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Sidebar(
                current: _section,
                onSelect: _select,
                onSearch: _search,
                searchController: _searchController,
                searchFocus: _searchFocus,
                syncActivity: _syncActivity,
                quota: _quota,
                onRefreshQuota: _refreshQuota,
                user: _authRepository.currentUser,
                updateAvailable: _updateAvailable,
                onLogout: () => _authRepository.logout(),
                isAdmin: _isAdmin,
              ),
              Expanded(
                child: Column(
                  children: [
                    Expanded(
                      child: Focus(
                        focusNode: _contentFocus,
                        child: IndexedStack(
                          index: sections.indexOf(_section),
                          children: [
                            for (final section in sections)
                              if (_visited.contains(section))
                                // Una sección oculta no debe recibir teclas ni
                                // archivos soltados (ver FileBrowserPage).
                                TickerMode(
                                  enabled: section == _section,
                                  child: ExcludeFocus(
                                    excluding: section != _section,
                                    child: _buildSection(section),
                                  ),
                                )
                              else
                                const SizedBox.shrink(),
                          ],
                        ),
                      ),
                    ),
                    TransferPanel(queue: _transfers),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
