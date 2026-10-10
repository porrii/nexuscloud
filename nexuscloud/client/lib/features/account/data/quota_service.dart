import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';

/// Uso y límite de almacenamiento de la cuenta (`GET /users/me/quota`,
/// §24, ADR-036). El uso es la huella real: archivos + papelera +
/// versiones anteriores.
class StorageQuota {
  const StorageQuota({
    required this.usedBytes,
    required this.filesBytes,
    required this.trashBytes,
    required this.versionsBytes,
    this.limitBytes,
  });

  factory StorageQuota.fromJson(Map<String, dynamic> json) {
    int read(String key) => (json[key] as num?)?.toInt() ?? 0;
    return StorageQuota(
      usedBytes: read('used_bytes'),
      filesBytes: read('files_bytes'),
      trashBytes: read('trash_bytes'),
      versionsBytes: read('versions_bytes'),
      limitBytes: (json['limit_bytes'] as num?)?.toInt(),
    );
  }

  final int usedBytes;
  final int filesBytes;
  final int trashBytes;
  final int versionsBytes;

  /// `null` si la cuenta no tiene límite.
  final int? limitBytes;

  /// Fracción usada (0..1), o `null` sin límite.
  double? get fraction {
    final limit = limitBytes;
    if (limit == null || limit <= 0) return null;
    return (usedBytes / limit).clamp(0, 1).toDouble();
  }
}

class QuotaService {
  QuotaService({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// Silencioso a propósito: devuelve `null` ante cualquier fallo (servidor
  /// antiguo sin el endpoint, red caída...). El medidor de la barra
  /// lateral es informativo, nunca debe interrumpir nada.
  Future<StorageQuota?> fetch() async {
    try {
      final response = await _apiClient.request(
        (dio) => dio.get<Map<String, dynamic>>('/users/me/quota'),
      );
      final data = response.data;
      return data == null ? null : StorageQuota.fromJson(data);
    } on ApiException {
      return null;
    }
  }
}
