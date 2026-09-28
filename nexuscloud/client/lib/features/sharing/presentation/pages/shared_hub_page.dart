import 'package:flutter/material.dart';

import '../../../../core/transfers/transfer_queue.dart';
import '../../../../core/widgets/page_scaffold.dart';
import 'my_shares_page.dart';
import 'shared_with_me_page.dart';

/// Sección "Compartido" de la barra lateral: lo que otros comparten
/// conmigo y lo que yo comparto, en dos pestañas (mismo reparto que
/// `web/src/pages/SharedPage.tsx`).
class SharedHubPage extends StatelessWidget {
  const SharedHubPage({super.key, this.transferQueue});

  final TransferQueue? transferQueue;

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        body: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const PageHeader(
              title: 'Compartido',
              subtitle: 'Carpetas y archivos que compartes o que otros comparten contigo.',
              padding: EdgeInsets.fromLTRB(28, 22, 28, 4),
            ),
            const SectionTabs(
              tabs: [
                (Icons.inbox_outlined, 'Conmigo'),
                (Icons.outbox_outlined, 'Por mí'),
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
                      SharedWithMePage(transferQueue: transferQueue),
                      const MySharesPage(),
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
