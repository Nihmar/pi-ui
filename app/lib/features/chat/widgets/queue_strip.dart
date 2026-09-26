import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/providers.dart';
import '../../../core/models/chat_entry.dart';
import '../../../core/theme/theme_tokens.dart';

/// The steer/follow-up queue above the composer.
///
/// pi owns the queue and the server exposes two operations on it: the queue is
/// read from `pi.queue_update` events and dropped as a whole with
/// `session.clear_queue`. There is no per-message cancel upstream, so the strip
/// does not pretend there is one.
class QueueStrip extends ConsumerWidget {
  const QueueStrip({super.key, required this.sessionId});

  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final queue =
        ref.watch(queueProvider(sessionId)).value ?? const <QueueItem>[];
    if (queue.isEmpty) {
      return const SizedBox.shrink();
    }
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceLg,
        vertical: tokens.spaceSm,
      ),
      decoration: BoxDecoration(
        color: tokens.surface,
        border: Border(top: BorderSide(color: tokens.border)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.queue, size: 14, color: tokens.warning),
              SizedBox(width: tokens.spaceXs),
              Text(
                '${queue.length} message${queue.length == 1 ? '' : 's'} waiting',
                style: theme.textTheme.labelSmall?.copyWith(
                  color: tokens.warning,
                ),
              ),
              const Spacer(),
              TextButton(
                onPressed: () =>
                    ref.read(sessionActionsProvider)?.clearQueue(sessionId),
                child: const Text('Clear'),
              ),
            ],
          ),
          Wrap(
            spacing: tokens.spaceXs,
            runSpacing: tokens.spaceXs,
            children: [for (final item in queue) _QueueChip(item: item)],
          ),
        ],
      ),
    );
  }
}

class _QueueChip extends StatelessWidget {
  const _QueueChip({required this.item});

  final QueueItem item;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final label = item.kind == 'steer' ? 'steer' : 'follow-up';
    return Container(
      constraints: const BoxConstraints(maxWidth: 420),
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceSm,
        vertical: tokens.spaceXs,
      ),
      decoration: BoxDecoration(
        color: tokens.surfaceAlt,
        borderRadius: BorderRadius.circular(tokens.radiusLg),
        border: Border.all(color: tokens.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            label,
            style: theme.textTheme.labelSmall?.copyWith(color: tokens.accent),
          ),
          SizedBox(width: tokens.spaceSm),
          Flexible(
            child: Text(
              item.text,
              style: theme.textTheme.bodySmall,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}
