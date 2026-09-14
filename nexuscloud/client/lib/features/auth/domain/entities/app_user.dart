import 'package:equatable/equatable.dart';

/// Usuario autenticado. Nunca lleva el token de sesión -- eso vive
/// exclusivamente en `TokenStore` (core/storage), para reducir a un único
/// punto dónde podría llegar a filtrarse (§167).
class AppUser extends Equatable {
  const AppUser({
    required this.id,
    required this.username,
    required this.displayName,
    required this.hasTotp,
    this.email,
  });

  final String id;
  final String username;
  final String displayName;
  final bool hasTotp;
  final String? email;

  @override
  List<Object?> get props => [id, username, displayName, hasTotp, email];
}
