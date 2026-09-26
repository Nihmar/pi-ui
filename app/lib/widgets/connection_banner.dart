import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/providers.dart';
import '../core/api/socket.dart';
import '../core/theme/theme_tokens.dart';

/// The strip that tells the truth about the connection.
///
/// It shows nothing while the socket is online — a connection that works is not
/// news — and a coloured line otherwise: amber while it retries with backoff,
/// red when it gave up waiting for a profile, plus a manual retry.
class ConnectionBanner extends ConsumerWidget {
  const ConnectionBanner({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(connectionProvider).value ?? SocketStatus.idle;
    if (status.isOnline || status == SocketStatus.idle) {
      return const SizedBox.shrink();
    }
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final color = status == SocketStatus.connecting
        ? tokens.textMuted
        : tokens.warning;
    final label = switch (status) {
      SocketStatus.connecting => 'Connecting to the server…',
      SocketStatus.reconnecting => 'Connection lost: retrying…',
      SocketStatus.idle => 'Not connected.',
      SocketStatus.online => '',
    };
    return Material(
      color: color.withValues(alpha: 0.12),
      child: Padding(
        padding: EdgeInsets.symmetric(
          horizontal: tokens.spaceLg,
          vertical: tokens.spaceSm,
        ),
        child: Row(
          children: [
            if (status.isWorking)
              SizedBox(
                width: 12,
                height: 12,
                child: CircularProgressIndicator(
                  strokeWidth: 1.5,
                  color: color,
                ),
              )
            else
              Icon(Icons.cloud_off, size: 14, color: color),
            SizedBox(width: tokens.spaceSm),
            Expanded(
              child: Text(
                label,
                style: theme.textTheme.labelSmall?.copyWith(color: color),
              ),
            ),
            TextButton(
              onPressed: () => ref.read(socketProvider)?.reconnectNow(),
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
