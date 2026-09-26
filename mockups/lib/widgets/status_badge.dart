import 'package:flutter/material.dart';

import '../core/models/session.dart';
import '../core/theme/theme_tokens.dart';

/// The lifecycle pill of a session: a dot, a word and the colour that goes with
/// the state. It is used on the list card, the session header and the chat.
class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.status, this.compact = false});

  /// The lifecycle state to render.
  final SessionStatus status;

  /// A denser pill for a list card (no icon, tighter padding).
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final (color, background) = _colors(tokens, status);
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: compact ? tokens.spaceSm : tokens.spaceMd,
        vertical: compact ? 2 : tokens.spaceXs,
      ),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(tokens.radiusLg),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (status == SessionStatus.spawning ||
              status == SessionStatus.streaming ||
              status == SessionStatus.stopping) ...[
            SizedBox(
              width: 10,
              height: 10,
              child: CircularProgressIndicator(strokeWidth: 1.5, color: color),
            ),
          ] else
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(color: color, shape: BoxShape.circle),
            ),
          SizedBox(width: tokens.spaceSm),
          Text(
            status.label,
            style: Theme.of(context).textTheme.labelSmall
                ?.copyWith(color: color, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }

  static (Color, Color) _colors(AppTokens tokens, SessionStatus status) {
    final color = switch (status) {
      SessionStatus.spawning => tokens.warning,
      SessionStatus.ready => tokens.success,
      SessionStatus.streaming => tokens.accent,
      SessionStatus.stopping => tokens.warning,
      SessionStatus.exited => tokens.textMuted,
      SessionStatus.crashed => tokens.error,
    };
    return (color, color.withValues(alpha: 0.12));
  }
}
