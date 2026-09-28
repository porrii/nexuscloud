import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';
import '../share_formatting.dart';

enum _LoadState { loading, loaded, error }

/// Todas mis comparticiones, sin filtrar por recurso -- equivalente a la
/// pestaña "Compartido por mí" de `web/src/pages/SharedPage.tsx`.
/// Solo esa mitad: "Compartido conmigo" es `SharedWithMePage`, y las dos se
/// muestran como pestañas de `SharedHubPage` (sección "Compartido").
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
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SubToolbar(
            leading: Text(
              _state == _LoadState.loaded
                  ? pluralize(_shares.length, 'compartición activa', 'comparticiones activas')
                  : 'Mis comparticiones',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: context.palette.textSecondary),
            ),
            actions: [
              IconButton(
                tooltip: 'Actualizar',
                icon: const Icon(Icons.refresh_rounded),
                onPressed: _load,
              ),
            ],
          ),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.error:
        return ErrorState(
          message: _errorMessage ?? 'No se pudo completar la operación.',
          onRetry: _load,
        );
      case _LoadState.loaded:
        if (_shares.isEmpty) {
          return const EmptyState(
            icon: Icons.outbox_outlined,
            title: 'Todavía no has compartido nada',
            message: 'Desde «Mis archivos», pasa el ratón sobre un elemento y '
                'pulsa «Compartir» para dar acceso a otra persona, a un grupo '
                'o crear un enlace.',
          );
        }
        return ListView(
          padding: const EdgeInsets.symmetric(vertical: 8),
          children: [
            for (final share in _shares)
              ShareRow(
                share: share,
                title: share.resourceName ?? share.resourceId,
                trailing: TextButton(
                  onPressed: () => _revoke(share),
                  style: TextButton.styleFrom(foregroundColor: context.palette.danger),
                  child: const Text('Revocar'),
                ),
              ),
          ],
        );
    }
  }
}

/// Fila de una compartición: recurso (o destinatario), a quién se da acceso
/// con su icono, permisos como pastillas y fecha. La usan "Compartido por
/// mí" y la lista de accesos del diálogo de compartir.
class ShareRow extends StatelessWidget {
  const ShareRow({
    super.key,
    required this.share,
    required this.trailing,
    this.title,
  });

  final Share share;

  /// Nombre del recurso ("Compartido por mí"). Sin él (diálogo de un
  /// recurso concreto) el título es el destinatario.
  final String? title;
  final Widget trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final targetIcon = switch (share.shareType) {
      ShareType.user => Icons.person_outline_rounded,
      ShareType.group => Icons.groups_outlined,
      ShareType.link => Icons.link_rounded,
    };
    final pills = [
      for (final part in shareMetadataLine(share).split(' · '))
        StatusPill(
          label: part,
          icon: part.startsWith('con contraseña')
              ? Icons.lock_outline_rounded
              : part.startsWith('expira')
                  ? Icons.schedule_rounded
                  : null,
        ),
    ];
    final resourceTitle = title;

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(bottom: BorderSide(color: p.border.withValues(alpha: 0.6))),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 12),
          child: Row(
            children: [
              if (resourceTitle != null)
                FileTypeIcon.forName(
                  resourceTitle,
                  isDirectory: share.resourceType == ShareResourceType.directory,
                  size: 20,
                  boxed: true,
                )
              else
                Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(color: p.accentSoft, shape: BoxShape.circle),
                  child: Icon(targetIcon, size: 18, color: p.accent),
                ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      resourceTitle ?? shareTargetLabel(share),
                      overflow: TextOverflow.ellipsis,
                      style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w500),
                    ),
                    if (resourceTitle != null) ...[
                      const SizedBox(height: 4),
                      Row(
                        children: [
                          Icon(targetIcon, size: 15, color: p.accent),
                          const SizedBox(width: 6),
                          Flexible(
                            child: Text(
                              shareTargetLabel(share),
                              overflow: TextOverflow.ellipsis,
                              style: text.bodyMedium?.copyWith(fontSize: 13, color: p.textSecondary),
                            ),
                          ),
                        ],
                      ),
                    ],
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 6,
                      runSpacing: 4,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        ...pills,
                        Text('Creado: ${formatShareDate(share.createdAt)}', style: text.bodySmall),
                      ],
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              trailing,
            ],
          ),
        ),
      ),
    );
  }
}
