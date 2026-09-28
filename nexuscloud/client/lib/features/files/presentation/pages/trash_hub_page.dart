import 'package:flutter/material.dart';

import '../../../../core/widgets/page_scaffold.dart';
import '../../../sync/presentation/pages/local_trash_page.dart';
import 'trash_page.dart';

/// Sección "Papelera" de la barra lateral: la del servidor y la local (lo
/// que la sincronización apartó de este equipo) en dos pestañas. Antes eran
/// dos iconos distintos en la barra superior, fáciles de confundir.
class TrashHubPage extends StatelessWidget {
  const TrashHubPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const DefaultTabController(
      length: 2,
      child: Scaffold(
        body: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            PageHeader(
              title: 'Papelera',
              subtitle: 'Recupera lo que borraste o elimínalo definitivamente.',
              padding: EdgeInsets.fromLTRB(28, 22, 28, 4),
            ),
            SectionTabs(
              tabs: [
                (Icons.cloud_outlined, 'En el servidor'),
                (Icons.computer_rounded, 'En este equipo'),
              ],
            ),
            Expanded(
              child: Padding(
                padding: EdgeInsets.fromLTRB(20, 16, 20, 16),
                child: Card(
                  clipBehavior: Clip.antiAlias,
                  child: TabBarView(
                    physics: NeverScrollableScrollPhysics(),
                    children: [TrashPage(), LocalTrashPage()],
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
