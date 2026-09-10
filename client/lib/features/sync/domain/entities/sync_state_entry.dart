import 'package:equatable/equatable.dart';

/// Estado de UN archivo sincronizado *a fecha de la última reconciliación
/// correcta* -- la "base" contra la que el modo [SyncDirection.both] decide
/// si un archivo cambió en remoto, en local, en ambos, o en ninguno
/// (ADR-012). Sin esta base no se puede distinguir un conflicto real
/// (§40) de una primera pasada sobre una carpeta ya poblada.
///
/// Tamaño remoto y local se guardan por separado: cuando un archivo está
/// en sync coinciden, pero tras dejar una *conflict copy* la base refleja
/// dos lados con tamaños distintos, y hace falta recordar cada uno para no
/// re-disparar el conflicto en la siguiente pasada.
///
/// `toJson`/`fromJson` viven aquí, no en un `data/models/` aparte como el
/// resto: esto nunca viaja por la API -- solo se serializa al manifiesto
/// JSON local (`FileSyncStateStore`) -- así que no hay ninguna frontera de
/// dominio que separar.
class SyncStateEntry extends Equatable {
  const SyncStateEntry({
    required this.remoteSizeBytes,
    required this.localSizeBytes,
    required this.sha256,
    required this.remoteUpdatedAt,
    required this.localModifiedAt,
  });

  final int remoteSizeBytes;
  final int localSizeBytes;

  /// Hash del contenido la última vez que quedó en sync (el del lado
  /// remoto tras resolver un conflicto). Se guarda para diagnóstico y
  /// posibles usos futuros; la decisión de "cambió / no cambió" se toma
  /// con tamaño+fecha, no rehashing.
  final String sha256;

  /// `updated_at` que el servidor reportaba para este archivo la última vez
  /// que quedó en sync. Siempre en UTC.
  final DateTime remoteUpdatedAt;

  /// Fecha de modificación del archivo local la última vez que quedó en
  /// sync -- la fija el propio motor (`File.setLastModified`) tras cada
  /// transferencia, igual que ya hacía el modo de solo descarga (ADR-011
  /// dec. 3). Siempre en UTC.
  final DateTime localModifiedAt;

  Map<String, dynamic> toJson() => {
        'remote_size_bytes': remoteSizeBytes,
        'local_size_bytes': localSizeBytes,
        'sha256': sha256,
        'remote_updated_at': remoteUpdatedAt.toUtc().toIso8601String(),
        'local_modified_at': localModifiedAt.toUtc().toIso8601String(),
      };

  /// Lanza si algún campo falta o no tiene el tipo esperado -- quien llama
  /// (`FileSyncStateStore`) trata cualquier error de parseo del manifiesto
  /// como "sin base" y sigue, nunca propaga.
  factory SyncStateEntry.fromJson(Map<String, dynamic> json) => SyncStateEntry(
        remoteSizeBytes: json['remote_size_bytes'] as int,
        localSizeBytes: json['local_size_bytes'] as int,
        sha256: json['sha256'] as String,
        remoteUpdatedAt: DateTime.parse(json['remote_updated_at'] as String),
        localModifiedAt: DateTime.parse(json['local_modified_at'] as String),
      );

  @override
  List<Object?> get props => [
        remoteSizeBytes,
        localSizeBytes,
        sha256,
        remoteUpdatedAt,
        localModifiedAt,
      ];
}
