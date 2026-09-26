import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/data/providers.dart';
import '../../core/models/chat_entry.dart';
import 'widgets/chat_timeline.dart';
import 'widgets/session_header.dart';

/// The conversation of one session: header, timeline and (in the next slice) the
/// composer with the steer/follow-up queue.
///
/// It never builds its own [Scaffold]: the compact route wraps it in one, the
/// desktop master/detail embeds it next to the list. That is what keeps the same
/// widget in both layouts.
class ChatScreen extends ConsumerWidget {
  const ChatScreen({super.key, required this.sessionId});

  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final entries =
        ref.watch(chatProvider(sessionId)).value ?? const <ChatEntry>[];
    return Column(
      children: [
        SessionHeader(sessionId: sessionId),
        Expanded(
          child: ChatTimeline(
            entries: entries,
            onAction: (action) {
              ScaffoldMessenger.of(context).showSnackBar(
                SnackBar(
                  content: Text(
                    'Action "$action" is not wired in the mockup yet',
                  ),
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}
