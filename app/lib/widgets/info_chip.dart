import 'package:flutter/material.dart';

import '../core/theme/theme_tokens.dart';

/// A small icon+label chip: provider, model, thinking level, queue count. One
/// widget, so a card and a header never disagree about how metadata looks.
class InfoChip extends StatelessWidget {
  const InfoChip({
    super.key,
    required this.icon,
    required this.label,
    this.color,
    this.ellipsis = true,
  });

  final IconData icon;
  final String label;

  /// Overrides the muted colour (a warning chip, for example).
  final Color? color;

  /// Whether a long label shrinks with an ellipsis instead of overflowing.
  final bool ellipsis;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final foreground = color ?? tokens.textMuted;
    return Container(
      padding: EdgeInsets.symmetric(horizontal: tokens.spaceSm, vertical: 2),
      decoration: BoxDecoration(
        color: tokens.surfaceAlt,
        borderRadius: BorderRadius.circular(tokens.radiusSm),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 12, color: foreground),
          SizedBox(width: tokens.spaceXs),
          if (ellipsis)
            Flexible(
              child: Text(
                label,
                style: Theme.of(context).textTheme.labelSmall
                    ?.copyWith(color: foreground),
                overflow: TextOverflow.ellipsis,
              ),
            )
          else
            Text(
              label,
              style: Theme.of(context).textTheme.labelSmall
                  ?.copyWith(color: foreground),
            ),
        ],
      ),
    );
  }
}
