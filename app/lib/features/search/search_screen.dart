import 'package:flutter/material.dart';

import '../../widgets/empty_state.dart';

/// Global search (files, sessions, messages). The result kinds land with the
/// screens that own them.
class SearchScreen extends StatelessWidget {
  const SearchScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Search')),
      body: const EmptyState(
        icon: Icons.search_outlined,
        title: 'Search everything',
        message: 'Files, sessions and messages share one search field.',
      ),
    );
  }
}
