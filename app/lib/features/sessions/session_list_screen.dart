import 'package:flutter/material.dart';

import '../../widgets/empty_state.dart';

/// The session list. The server wiring lands with the client slice; this screen
/// fixes the branch, the app bar and the empty state.
class SessionListScreen extends StatelessWidget {
  const SessionListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Sessions'),
        actions: [
          IconButton(
            onPressed: () {},
            tooltip: 'New session',
            icon: const Icon(Icons.add),
          ),
        ],
      ),
      body: const EmptyState(
        icon: Icons.forum_outlined,
        title: 'No sessions yet',
        message: 'A session is one pi process in one working directory.',
      ),
    );
  }
}
