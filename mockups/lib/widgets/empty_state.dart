import 'package:flutter/material.dart';

import '../core/theme/theme_tokens.dart';

/// The one empty state of the mockup: an icon, a title, a line of explanation and
/// optionally an action. Every empty list, pane and search result uses it, so the
/// reviewer sees one voice between screens.
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    this.message,
    this.action,
    this.compact = false,
  });

  /// The glyph shown above the title.
  final IconData icon;

  /// The short headline.
  final String title;

  /// The explanatory line under the title.
  final String? message;

  /// An optional call to action.
  final Widget? action;

  /// A denser variant for a pane rather than a whole screen.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 360),
        child: Padding(
          padding: EdgeInsets.all(compact ? tokens.spaceLg : tokens.spaceXl),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: compact ? 28 : 40, color: tokens.textDim),
              SizedBox(height: tokens.spaceMd),
              Text(
                title,
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              if (message != null) ...[
                SizedBox(height: tokens.spaceSm),
                Text(
                  message!,
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
              if (action != null) ...[
                SizedBox(height: tokens.spaceLg),
                action!,
              ],
            ],
          ),
        ),
      ),
    );
  }
}
