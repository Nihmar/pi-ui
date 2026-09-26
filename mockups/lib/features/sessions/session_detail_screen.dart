import 'package:flutter/material.dart';

import '../../core/theme/breakpoints.dart';
import '../chat/chat_screen.dart';
import 'session_list_screen.dart';

/// The `:id` nested route of the sessions branch.
///
/// On a phone it is the chat, full screen, above the list. From the desktop
/// breakpoint it is the same master/detail row with the session selected, so the
/// layout — not the route — decides how much is on screen.
class SessionDetailScreen extends StatelessWidget {
  const SessionDetailScreen({super.key, required this.sessionId});

  final String sessionId;

  @override
  Widget build(BuildContext context) {
    if (context.isExpanded) {
      return Row(
        children: [
          SizedBox(width: 380, child: SessionListPane(selectedId: sessionId)),
          const VerticalDivider(width: 1),
          Expanded(child: ChatScreen(sessionId: sessionId)),
        ],
      );
    }
    return Scaffold(
      body: SafeArea(child: ChatScreen(sessionId: sessionId)),
    );
  }
}
