import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/data/providers.dart';
import '../core/models/scenario.dart';
import '../core/theme/theme_tokens.dart';

/// The global connection banner.
///
/// It is the first thing under the app bar in every layout: a client that keeps
/// composing while the socket is gone must see why its messages are not moving.
/// An online connection renders nothing at all.
class ConnectionBanner extends ConsumerWidget {
  const ConnectionBanner({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final connection =
        ref.watch(connectionProvider).value ?? MockConnection.online;
    if (connection == MockConnection.online) {
      return const SizedBox.shrink();
    }
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final offline = connection == MockConnection.offline;
    final color = offline ? tokens.error : tokens.warning;
    return Container(
      width: double.infinity,
      color: color.withValues(alpha: 0.12),
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceLg,
        vertical: tokens.spaceSm,
      ),
      child: Row(
        children: [
          if (offline)
            Icon(Icons.cloud_off_outlined, size: 16, color: color)
          else
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(strokeWidth: 1.5, color: color),
            ),
          SizedBox(width: tokens.spaceSm),
          Expanded(
            child: Text(
              offline
                  ? 'Offline — messages are queued and sent when the connection returns.'
                  : 'Reconnecting — the server will replay the events you missed.',
              style: theme.textTheme.labelSmall?.copyWith(color: color),
            ),
          ),
          if (offline)
            TextButton(
              onPressed: () => ref
                  .read(mockApiProvider)
                  .setConnection(MockConnection.reconnecting),
              child: const Text('Reconnect'),
            ),
        ],
      ),
    );
  }
}
