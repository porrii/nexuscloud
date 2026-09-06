import 'package:equatable/equatable.dart';

/// Un único par carpeta-remota/carpeta-local a sincronizar (slice A: solo
/// se admite uno a la vez -- ver ADR-011).
class SyncPair extends Equatable {
  const SyncPair({required this.remotePath, required this.localPath});

  final String remotePath;
  final String localPath;

  @override
  List<Object?> get props => [remotePath, localPath];
}
