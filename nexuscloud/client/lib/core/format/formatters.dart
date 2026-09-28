/// Formateo de tamaños y fechas compartido por todas las pantallas, en vez
/// de una copia de `_formatSize`/`_formatDate` en cada página. Sin el
/// paquete `intl` a propósito: la app solo habla español y estas pocas
/// reglas no justifican una dependencia más.
library;

const _monthsShort = [
  'ene',
  'feb',
  'mar',
  'abr',
  'may',
  'jun',
  'jul',
  'ago',
  'sep',
  'oct',
  'nov',
  'dic',
];

String _two(int n) => n.toString().padLeft(2, '0');

/// `2048` → `2.0 KB`, `1536000` → `1.5 MB` -- mismo formato exacto que
/// `formatBytes` de la web (`web/src/format.ts`), para que el mismo archivo
/// muestre el mismo tamaño en los dos clientes.
String formatBytes(int bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var size = bytes.toDouble();
  var unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  final decimals = (unitIndex == 0 || size >= 10) ? 0 : 1;
  return '${size.toStringAsFixed(decimals)} ${units[unitIndex]}';
}

/// Fecha absoluta completa: `2026-09-27 14:05` (hora local).
String formatDateTime(DateTime dateTime) {
  final local = dateTime.toLocal();
  return '${local.year}-${_two(local.month)}-${_two(local.day)} '
      '${_two(local.hour)}:${_two(local.minute)}';
}

/// Fecha corta y legible para columnas de listado: `hace 5 min`,
/// `hoy, 14:05`, `ayer, 09:10`, `12 sep`, `12 sep 2025`.
String formatRelativeDate(DateTime dateTime, {DateTime? now}) {
  final local = dateTime.toLocal();
  final current = (now ?? DateTime.now()).toLocal();
  final diff = current.difference(local);

  if (!diff.isNegative && diff.inSeconds < 60) return 'ahora mismo';
  if (!diff.isNegative && diff.inMinutes < 60) {
    return 'hace ${diff.inMinutes} min';
  }

  final today = DateTime(current.year, current.month, current.day);
  final day = DateTime(local.year, local.month, local.day);
  final dayDiff = today.difference(day).inDays;
  final time = '${_two(local.hour)}:${_two(local.minute)}';
  if (dayDiff == 0) return 'hoy, $time';
  if (dayDiff == 1) return 'ayer, $time';

  final month = _monthsShort[local.month - 1];
  if (local.year == current.year) return '${local.day} $month';
  return '${local.day} $month ${local.year}';
}

/// "hace un momento" / "hace 5 min" / "hace 2 h" / fecha -- para estados
/// del tipo "última sincronización".
String formatAgo(DateTime dateTime, {DateTime? now}) {
  final current = (now ?? DateTime.now()).toLocal();
  final diff = current.difference(dateTime.toLocal());
  if (diff.isNegative || diff.inSeconds < 45) return 'hace un momento';
  if (diff.inMinutes < 60) return 'hace ${diff.inMinutes} min';
  if (diff.inHours < 24) return 'hace ${diff.inHours} h';
  return formatRelativeDate(dateTime, now: now);
}

/// Plural simple: `pluralize(1, 'archivo')` → `1 archivo`,
/// `pluralize(3, 'carpeta')` → `3 carpetas`.
String pluralize(int count, String singular, [String? plural]) {
  final word = count == 1 ? singular : (plural ?? '${singular}s');
  return '$count $word';
}
