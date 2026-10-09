import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../data/admin_service.dart';
import '../tabs/audit_tab.dart';
import '../tabs/groups_tab.dart';
import '../tabs/invitations_tab.dart';
import '../tabs/system_tab.dart';
import '../tabs/users_tab.dart';

/// Sección «Administración» (§56), solo visible para administradores. Fase A:
/// todo lo que la API ya permite (usuarios, grupos, invitaciones,
/// auditoría, discos y cola de miniaturas). Backups, pools, sesiones de
/// otros usuarios, roles y estado del sistema necesitan endpoints nuevos.
class AdminPage extends StatelessWidget {
  const AdminPage({super.key, this.currentUserId, this.service});

  final String? currentUserId;

  /// Por defecto, el registrado en el localizador.
  final AdminService? service;

  @override
  Widget build(BuildContext context) {
    final admin = service ?? sl<AdminService>();
    return DefaultTabController(
      length: 5,
      child: Scaffold(
        body: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const PageHeader(
              title: 'Administración',
              subtitle:
                  'Cuentas, grupos, invitaciones, auditoría y estado del '
                  'servidor.',
              padding: EdgeInsets.fromLTRB(28, 22, 28, 4),
            ),
            const SectionTabs(
              tabs: [
                (Icons.person_outline_rounded, 'Usuarios'),
                (Icons.groups_outlined, 'Grupos'),
                (Icons.mail_outline_rounded, 'Invitaciones'),
                (Icons.fact_check_outlined, 'Auditoría'),
                (Icons.dns_outlined, 'Sistema'),
              ],
            ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(20, 16, 20, 16),
                child: Card(
                  clipBehavior: Clip.antiAlias,
                  child: TabBarView(
                    physics: const NeverScrollableScrollPhysics(),
                    children: [
                      UsersTab(service: admin, currentUserId: currentUserId),
                      GroupsTab(service: admin),
                      InvitationsTab(service: admin),
                      AuditTab(service: admin),
                      SystemTab(service: admin),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
