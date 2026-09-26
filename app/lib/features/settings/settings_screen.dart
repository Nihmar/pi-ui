import 'package:flutter/material.dart';

import '../../widgets/empty_state.dart';

/// Settings. The sections (server, trust, TLS, themes, languages) arrive with
/// their screens; this fixes the branch and the app bar.
class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: const EmptyState(
        icon: Icons.settings_outlined,
        title: 'Nothing to configure yet',
        message: 'Server, trust, TLS, themes and language settings land here.',
      ),
    );
  }
}
