import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/data/providers.dart';
import '../../core/models/session.dart';
import '../../core/router.dart';
import '../../core/theme/breakpoints.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';
import 'widgets/new_session_sheet.dart';
import 'widgets/session_card.dart';

/// The sessions branch.
///
/// On a phone it is the list, full screen. From the desktop breakpoint it is the
/// master pane of a master/detail layout; the detail is the nested `:id` route.
class SessionListScreen extends ConsumerWidget {
  const SessionListScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final pane = const SessionListPane();
    return Scaffold(
      appBar: AppBar(
        title: const Text('Sessions'),
        actions: [
          IconButton(
            onPressed: () => _createSession(context),
            tooltip: 'New session',
            icon: const Icon(Icons.add),
          ),
        ],
      ),
      body: context.isExpanded
          ? Row(
              children: [
                const SizedBox(width: 380, child: SessionListPane()),
                const VerticalDivider(width: 1),
                const Expanded(
                  child: EmptyState(
                    icon: Icons.chat_bubble_outline,
                    title: 'Select a session',
                    message: 'Pick a session on the left, or start a new one.',
                  ),
                ),
              ],
            )
          : pane,
    );
  }

  static Future<void> _createSession(BuildContext context) async {
    final sessionId = await showNewSessionSheet(context);
    if (sessionId != null && context.mounted) {
      context.go(Routes.chat(sessionId));
    }
  }
}

/// The list itself, without a scaffold: the phone branch and the desktop master
/// pane render the same widget.
class SessionListPane extends ConsumerWidget {
  const SessionListPane({super.key, this.selectedId});

  /// The session whose card is highlighted in the desktop layout.
  final String? selectedId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessions =
        ref.watch(sessionsProvider).value ?? const <SessionModel>[];
    final api = ref.read(mockApiProvider);
    if (sessions.isEmpty) {
      return const EmptyState(
        icon: Icons.forum_outlined,
        title: 'No sessions yet',
        message: 'A session is one pi process in one working directory.',
      );
    }
    return ListView.builder(
      padding: EdgeInsets.symmetric(vertical: context.tokens.spaceSm),
      itemCount: sessions.length,
      itemBuilder: (context, index) {
        final session = sessions[index];
        return SessionCard(
          session: session,
          selected: session.id == selectedId,
          onTap: () => context.go(Routes.chat(session.id)),
          onStop: session.status.isLive
              ? () => api.stopSession(session.id)
              : null,
          onRemove: () => api.removeSession(session.id),
        );
      },
    );
  }
}
