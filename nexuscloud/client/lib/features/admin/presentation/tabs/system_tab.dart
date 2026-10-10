import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/admin_models.dart';
import '../../data/admin_service.dart';

/// Estado del servidor que la API ya expone: discos y la cola de
/// miniaturas (§34, ADR-041). CPU, RAM, backups y pools llegarán con los
/// endpoints de la fase B.
class SystemTab extends StatefulWidget {
  const SystemTab({super.key, required this.service});

  final AdminService service;

  @override
  State<SystemTab> createState() => _SystemTabState();
}

class _SystemTabState extends State<SystemTab> {
  List<DiskInfo>? _disks;
  bool _disksUnsupported = false;
  List<ThumbnailJob>? _pending;
  List<ThumbnailJob>? _failed;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final results = await Future.wait([
        widget.service.listDisks(),
        widget.service.listThumbnailJobs(failed: false),
        widget.service.listThumbnailJobs(failed: true),
      ]);
      if (!mounted) return;
      setState(() {
        final disks = results[0] as List<DiskInfo>?;
        _disksUnsupported = disks == null;
        _disks = disks ?? const [];
        _pending = results[1] as List<ThumbnailJob>;
        _failed = results[2] as List<ThumbnailJob>;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null) return ErrorState(message: _error!, onRetry: _load);
    if (_disks == null) return const LoadingState();
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        SectionCard(
          title: 'Almacenamiento',
          icon: Icons.storage_rounded,
          description: 'Unidades montadas en el servidor.',
          trailing: IconButton(
            tooltip: 'Actualizar',
            onPressed: _load,
            icon: const Icon(Icons.refresh_rounded),
          ),
          child: _buildDisks(context),
        ),
        const SizedBox(height: 16),
        SectionCard(
          title: 'Miniaturas',
          icon: Icons.photo_library_outlined,
          description:
              'Cola de generación. Un trabajo fallido suele indicar que falta '
              'ffmpeg o pdftoppm en el servidor, o un archivo dañado.',
          child: _buildThumbnails(context),
        ),
      ],
    );
  }

  Widget _buildDisks(BuildContext context) {
    final text = Theme.of(context).textTheme;
    if (_disksUnsupported) {
      return Text(
        'El sistema operativo del servidor no permite enumerar sus discos.',
        style: text.bodySmall,
      );
    }
    final disks = _disks!;
    if (disks.isEmpty) {
      return Text('No se encontró ninguna unidad.', style: text.bodySmall);
    }
    return Column(
      children: [
        for (final disk in disks) ...[
          _DiskRow(disk: disk),
          if (disk != disks.last) const SizedBox(height: 14),
        ],
      ],
    );
  }

  Widget _buildThumbnails(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final pending = _pending ?? const [];
    final failed = _failed ?? const [];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 8,
          children: [
            StatusPill(
              label: '${pending.length} pendientes',
              icon: Icons.schedule_rounded,
              color: p.textSecondary,
              background: p.surfaceMuted,
            ),
            StatusPill(
              label: '${failed.length} fallidas',
              icon: Icons.error_outline_rounded,
              color: failed.isEmpty ? p.success : p.danger,
              background: failed.isEmpty ? p.successSoft : p.dangerSoft,
            ),
          ],
        ),
        if (failed.isNotEmpty) ...[
          const SizedBox(height: 12),
          for (final job in failed.take(20))
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                '${job.kind} · ${pluralize(job.attempts, 'intento')} · '
                '${job.lastError ?? 'sin detalle'}',
                style: monoTextStyle.copyWith(
                  fontSize: 12,
                  color: p.textSecondary,
                ),
              ),
            ),
          if (failed.length > 20)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                'y ${failed.length - 20} más…',
                style: text.bodySmall,
              ),
            ),
        ],
      ],
    );
  }
}

class _DiskRow extends StatelessWidget {
  const _DiskRow({required this.disk});

  final DiskInfo disk;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final fraction = disk.fraction;
    final color = fraction >= 0.9
        ? p.danger
        : (fraction >= 0.75 ? p.warning : p.accent);
    final name = [
      disk.mountPoint,
      if (disk.label != null && disk.label!.isNotEmpty) disk.label!,
      if (disk.filesystem != null && disk.filesystem!.isNotEmpty)
        disk.filesystem!,
    ].join(' · ');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                name,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
              ),
            ),
            if (disk.readOnly) ...[
              StatusPill(
                label: 'Solo lectura',
                color: p.warning,
                background: p.warningSoft,
              ),
              const SizedBox(width: 8),
            ],
            Text(
              '${formatBytes(disk.usedBytes)} de ${formatBytes(disk.totalBytes)}',
              style: text.bodySmall,
            ),
          ],
        ),
        const SizedBox(height: 6),
        ClipRRect(
          borderRadius: BorderRadius.circular(4),
          child: LinearProgressIndicator(
            value: fraction,
            minHeight: 8,
            color: color,
            backgroundColor: p.surfaceMuted,
          ),
        ),
        const SizedBox(height: 4),
        Text('${formatBytes(disk.freeBytes)} libres', style: text.bodySmall),
      ],
    );
  }
}
