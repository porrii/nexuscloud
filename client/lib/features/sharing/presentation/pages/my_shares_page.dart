import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';
import '../share_formatting.dart';

enum _LoadState { loading, loaded, error }

/// Todas mis comparticiones, sin filtrar por recurso -- equivalente a la
/// pestaña "Compartido por mí" de `web/src/pages/SharedPage.tsx`.
/// Deliberadamente solo esa mitad: "Compartido conmigo" (navegar carpetas
/// que otros me compartieron) es un slice futuro, no una pestaña de esta
/// misma página -- por eso no hay selector con/por mí aquí.
class MySharesPage extends StatefulWidget {
  const MySharesPage({super.key});

  @override
  State<MySharesPage> createState() => _MySharesPageState();
}

class _MySharesPageState extends State<MySharesPage> {
  final SharingRepository _sharingRepository = sl<SharingRepository>();

  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  List<Share> _shares = [];

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _state = _LoadState.loading);
    try {
      final shares =
          await _sharingRepository.listShares(direction: ShareDirection.byMe);
      if (!mounted) return;
      setState(() {
        _shares = shares;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _revoke(Share share) async {
    final confirmed = await showConfirmDialog(
      context,
      title: 'Revocar compartición',
      message: 'Se revocará el acceso a "${shareTargetLabel(share)}". '
          'Dejará de poder acceder de inmediato y no podrá deshacerse. '
          '¿Continuar?',
      confirmLabel: 'Revocar',
      danger: true,
    );
    if (!confirmed) return;

    try {
      await _sharingRepository.revokeShare(share.id);
      if (mounted) await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Mis comparticiones'),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const Center(child: CircularProgressIndicator());
      case _LoadState.error:
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_errorMessage ?? 'No se pudo completar la operación.'),
                const SizedBox(height: 12),
                OutlinedButton(onPressed: _load, child: const Text('Reintentar')),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        if (_shares.isEmpty) {
          return const Center(child: Text('Todavía no has compartido nada'));
        }
        return ListView(
          children: [
            for (final share in _shares)
              ListTile(
                leading: Icon(
                  share.resourceType == ShareResourceType.directory
                      ? Icons.folder
                      : Icons.insert_drive_file,
                ),
                title: Text(share.resourceName ?? share.resourceId),
                subtitle: Text(
                  '${shareTargetLabel(share)}\n'
                  '${shareMetadataLine(share)}\n'
                  'Creado: ${formatShareDate(share.createdAt)}',
                ),
                isThreeLine: true,
                trailing: TextButton(
                  onPressed: () => _revoke(share),
                  child: const Text('Revocar'),
                ),
              ),
          ],
        );
    }
  }
}
