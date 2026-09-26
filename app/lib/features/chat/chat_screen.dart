import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/providers.dart';
import '../../core/api/session_stats.dart';
import 'widgets/chat_timeline.dart';
import 'widgets/composer.dart';
import 'widgets/dialog_card.dart';
import 'widgets/queue_strip.dart';
import 'widgets/session_header.dart';

/// The conversation of one session: header, timeline, the dialog the run waits
/// on, the queue strip and the composer.
///
/// It never builds its own [Scaffold]: the compact route wraps it in one, the
/// desktop master/detail embeds it next to the list. That is what keeps the same
/// widget in both layouts.
class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  SessionStats _stats = const SessionStats();
  var _statsLoaded = false;

  /// Dialogs the user already answered: the card goes immediately, while the
  /// server's own `request` stays in the fold until a new one replaces it.
  final _answeredDialogs = <String>{};

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _loadStats());
  }

  @override
  void didUpdateWidget(covariant ChatScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.sessionId != widget.sessionId) {
      _stats = const SessionStats();
      _statsLoaded = false;
      _answeredDialogs.clear();
      WidgetsBinding.instance.addPostFrameCallback((_) => _loadStats());
    }
  }

  /// Asks the child for the numbers the projection does not carry.
  ///
  /// Called once per open and again whenever a run settles: a cost or a context
  /// figure is a snapshot by nature, not a stream.
  Future<void> _loadStats() async {
    if (_statsLoaded) {
      return;
    }
    final actions = ref.read(sessionActionsProvider);
    if (actions == null) {
      return;
    }
    _statsLoaded = true;
    try {
      final state = await actions.state(widget.sessionId);
      final stats = await actions.stats(widget.sessionId);
      if (!mounted) {
        return;
      }
      var merged = _stats;
      if (state != null) {
        merged = merged.merge(SessionStats.fromState(state));
      }
      if (stats != null) {
        merged = merged.merge(SessionStats.fromSessionStats(stats));
      }
      setState(() => _stats = merged);
    } on PiuiException {
      // The child may be gone (an exited session): the header falls back to the
      // projection, which is why this failure is not worth a banner.
      _statsLoaded = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final sessionId = widget.sessionId;
    // A settled run is exactly when the numbers changed: refresh them then.
    ref.listen(streamingProvider(sessionId), (previous, next) {
      if (previous == true && next == false) {
        _statsLoaded = false;
        unawaited(_loadStats());
      }
    });
    final entries = ref.watch(chatProvider(sessionId)).value ?? const [];
    final dialog = ref.watch(dialogProvider(sessionId)).value;
    return Column(
      children: [
        SessionHeader(sessionId: sessionId, stats: _stats),
        Expanded(
          child: ChatTimeline(
            entries: entries,
            onAction: (action) => _runAction(action),
          ),
        ),
        // The dialog sits between the conversation and the composer: it belongs
        // to the run, not to a modal layer over it.
        if (dialog != null && !_answeredDialogs.contains(dialog.id))
          DialogCard(
            sessionId: sessionId,
            request: dialog,
            onAnswered: (id) => setState(() => _answeredDialogs.add(id)),
          ),
        QueueStrip(sessionId: sessionId),
        Composer(sessionId: sessionId),
      ],
    );
  }

  /// Runs one action an entry offered (a retry, a copy, a stop).
  void _runAction(String action) {
    final messenger = ScaffoldMessenger.of(context);
    switch (action) {
      case 'abort':
        ref.read(sessionActionsProvider)?.abort(widget.sessionId).catchError((
          Object error,
        ) {
          messenger.showSnackBar(SnackBar(content: Text('$error')));
        });
      case 'refresh':
        ref.invalidate(sessionsProvider);
      default:
        messenger.showSnackBar(
          SnackBar(content: Text('"$action" lands in a later slice')),
        );
    }
  }
}
