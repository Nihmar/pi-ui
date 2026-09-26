import 'package:flutter/material.dart';

import '../core/theme/theme_tokens.dart';

/// The context and cost strip of a session: how full the context window is and
/// what the session cost so far. One widget, so the list card, the header and the
/// statistics screen cannot disagree.
class ContextBar extends StatelessWidget {
  const ContextBar({
    super.key,
    required this.usedTokens,
    required this.windowTokens,
    required this.costUsd,
    this.showLabels = true,
  });

  /// Tokens currently in the context window.
  final int usedTokens;

  /// The model's context window.
  final int windowTokens;

  /// Session cost so far.
  final double costUsd;

  /// Whether the numbers are printed next to the bar.
  final bool showLabels;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final fraction = windowTokens <= 0
        ? 0.0
        : (usedTokens / windowTokens).clamp(0.0, 1.0);
    final color = fraction > 0.85
        ? tokens.error
        : fraction > 0.6
        ? tokens.warning
        : tokens.accent;
    final bar = ClipRRect(
      borderRadius: BorderRadius.circular(tokens.radiusSm),
      child: SizedBox(
        height: 6,
        child: LinearProgressIndicator(
          value: fraction,
          backgroundColor: tokens.surfaceAlt,
          valueColor: AlwaysStoppedAnimation<Color>(color),
        ),
      ),
    );

    if (!showLabels) {
      return bar;
    }

    return Row(
      children: [
        Expanded(child: bar),
        SizedBox(width: tokens.spaceMd),
        Text(
          '${_k(usedTokens)} / ${_k(windowTokens)} · \$${costUsd.toStringAsFixed(2)}',
          style: theme.textTheme.labelSmall,
        ),
      ],
    );
  }

  /// Renders a token count in the `12.4k` form a status bar has room for.
  static String _k(int value) {
    if (value < 1000) {
      return '$value';
    }
    final thousands = value / 1000;
    return '${thousands.toStringAsFixed(thousands < 10 ? 1 : 0)}k';
  }
}
