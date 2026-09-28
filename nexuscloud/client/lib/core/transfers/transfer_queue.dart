import 'dart:async';

import 'package:flutter/foundation.dart';

enum TransferKind { upload, download }

enum TransferStatus { running, done, failed }

/// Una transferencia visible en el panel inferior -- estado solo de
/// presentación, nunca persiste ni se comparte con el dominio.
class TransferItem {
  TransferItem._({
    required this.id,
    required this.kind,
    required this.name,
    this.detail,
  });

  final int id;
  final TransferKind kind;
  final String name;

  /// Destino legible ("a /Fotos", "en Descargas") para la lista expandida.
  final String? detail;
  double progress = 0;
  TransferStatus status = TransferStatus.running;
  String? error;
}

/// Cola única de subidas y descargas de toda la app. Antes cada pantalla
/// pintaba su propia lista encima del contenido; ahora todas informan aquí
/// y el panel inferior del shell la muestra, siga el usuario en esa
/// pantalla o no.
///
/// Las terminadas con éxito desaparecen solas tras [doneLinger]; las
/// fallidas se quedan hasta que el usuario las descarta (mismo criterio que
/// antes: el error de una transferencia se ve en su propia fila).
class TransferQueue extends ChangeNotifier {
  TransferQueue({this.doneLinger = const Duration(seconds: 4)});

  final Duration doneLinger;
  final List<TransferItem> _items = [];
  final Set<Timer> _timers = {};
  int _nextId = 0;
  bool _disposed = false;

  List<TransferItem> get items => List.unmodifiable(_items);

  int get activeCount =>
      _items.where((t) => t.status == TransferStatus.running).length;

  int get failedCount =>
      _items.where((t) => t.status == TransferStatus.failed).length;

  /// Progreso medio de las que siguen en curso, o `null` si ninguna ha
  /// informado todavía de su tamaño.
  double? get overallProgress {
    final running = _items.where((t) => t.status == TransferStatus.running);
    if (running.isEmpty) return null;
    final total = running.fold<double>(0, (sum, t) => sum + t.progress);
    final value = total / running.length;
    return value == 0 ? null : value;
  }

  TransferItem start(TransferKind kind, String name, {String? detail}) {
    final item = TransferItem._(
      id: _nextId++,
      kind: kind,
      name: name,
      detail: detail,
    );
    _items.add(item);
    _notify();
    return item;
  }

  void progress(TransferItem item, int done, int total) {
    if (total <= 0) return;
    item.progress = done / total;
    _notify();
  }

  void succeed(TransferItem item) {
    item
      ..status = TransferStatus.done
      ..progress = 1;
    _notify();
    late final Timer timer;
    timer = Timer(doneLinger, () {
      _timers.remove(timer);
      if (_items.remove(item)) _notify();
    });
    _timers.add(timer);
  }

  void fail(TransferItem item, String message) {
    item
      ..status = TransferStatus.failed
      ..error = message;
    _notify();
  }

  void dismiss(TransferItem item) {
    if (_items.remove(item)) _notify();
  }

  void clearFinished() {
    _items.removeWhere((t) => t.status != TransferStatus.running);
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    for (final timer in _timers) {
      timer.cancel();
    }
    _timers.clear();
    super.dispose();
  }
}
