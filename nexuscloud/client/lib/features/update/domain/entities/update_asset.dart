/// Un asset del feed de Velopack (`releases.json` reenviado por el propio
/// servidor NexusCloud, ver ADR-032). Los nombres de campo del JSON real
/// (`PackageId`, `SHA256`, ...) van en mayúscula porque así los escribe
/// `vpk pack` -- no es un capricho de este cliente.
class UpdateAsset {
  const UpdateAsset({
    required this.packageId,
    required this.version,
    required this.type,
    required this.fileName,
    required this.sha256,
    required this.size,
  });

  factory UpdateAsset.fromJson(Map<String, dynamic> json) => UpdateAsset(
        packageId: json['PackageId'] as String,
        version: json['Version'] as String,
        type: json['Type'] as String,
        fileName: json['FileName'] as String,
        sha256: json['SHA256'] as String,
        size: (json['Size'] as num).toInt(),
      );

  final String packageId;
  final String version;

  /// "Full" o "Delta" -- este cliente solo aplica "Full" (sin soporte de
  /// actualizaciones delta todavía, ver el plan de esta feature).
  final String type;
  final String fileName;
  final String sha256;
  final int size;

  bool get isFull => type == 'Full';
}
