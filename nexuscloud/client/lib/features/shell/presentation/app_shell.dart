import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/di/service_locator.dart';
import '../../../core/transfers/transfer_panel.dart';
import '../../../core/transfers/transfer_queue.dart';
import '../../account/data/quota_service.dart';
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

  ShellSection _section = ShellSection.files;
  final Set<ShellSection> _visited = {ShellSection.files};
  String _searchQuery = '';
  int _searchGeneration = 0;
  StorageQuota? _quota;
  bool _updateAvailable = false;
  int _lastActiveTransfers = 0;

  @override
  void initState() {
    super.initState();
    _syncActivity.init();
    _transfers.addListener(_onTransfersChanged);
    _refreshQuota();
    _checkForUpdatesSilently();
  }

  @override
  void dispose() {
    _transfers.removeListener(_onTransfersChanged);
    _searchController.dispose();
    _searchFocus.dispose();
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
    return CallbackShortcuts(
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
        const SingleActivator(LogicalKeyboardKey.keyK, control: true): () {
          _searchFocus.requestFocus();
          _searchController.selection = TextSelection(
            baseOffset: 0,
            extentOffset: _searchController.text.length,
          );
        },
      },
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
            ),
            Expanded(
              child: Column(
                children: [
                  Expanded(
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
                  TransferPanel(queue: _transfers),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
