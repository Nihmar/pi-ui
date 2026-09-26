import 'package:flutter/material.dart';

import '../../../core/models/chat_entry.dart';
import '../../../core/theme/theme_tokens.dart';
import '../../../widgets/empty_state.dart';
import 'entry_tile.dart';

/// The scrolling timeline of one session.
///
/// It stays pinned to the newest entry while the user is near the bottom, and
/// stops following as soon as the user scrolls up to read; that is the one
/// behaviour every chat client has to get right.
class ChatTimeline extends StatefulWidget {
  const ChatTimeline({super.key, required this.entries, this.onAction});

  final List<ChatEntry> entries;
  final void Function(String action)? onAction;

  @override
  State<ChatTimeline> createState() => _ChatTimelineState();
}

class _ChatTimelineState extends State<ChatTimeline> {
  final _controller = ScrollController();

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onScroll);
  }

  @override
  void dispose() {
    _controller.removeListener(_onScroll);
    _controller.dispose();
    super.dispose();
  }

  void _onScroll() {
    // Nothing to maintain: the pinned flag is read at the next update.
  }

  @override
  void didUpdateWidget(covariant ChatTimeline oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.entries.length != oldWidget.entries.length) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _maybeFollow());
    }
  }

  void _maybeFollow() {
    if (!mounted || !_controller.hasClients) {
      return;
    }
    final position = _controller.position;
    // Follow only when the reader is already at the bottom: scrolling up is a
    // request to stay where they are.
    if (position.extentAfter > 160) {
      return;
    }
    _controller.animateTo(
      position.maxScrollExtent,
      duration: const Duration(milliseconds: 120),
      curve: Curves.easeOut,
    );
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    if (widget.entries.isEmpty) {
      return const EmptyState(
        icon: Icons.chat_bubble_outline,
        title: 'No messages yet',
        message: 'Send a prompt to start the conversation.',
      );
    }
    return ListView.separated(
      controller: _controller,
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceLg,
        vertical: tokens.spaceMd,
      ),
      itemCount: widget.entries.length,
      separatorBuilder: (context, index) => SizedBox(height: tokens.spaceMd),
      itemBuilder: (context, index) => EntryTile(
        key: ValueKey(widget.entries[index].id),
        entry: widget.entries[index],
        onAction: widget.onAction,
      ),
    );
  }
}
