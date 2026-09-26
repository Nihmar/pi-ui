import 'package:flutter/material.dart';

import '../../../core/models/chat_entry.dart';
import '../../../core/theme/theme_tokens.dart';

/// The file diff of an edit tool call: a header with the path and the counts,
/// then the lines, each tinted by its kind.
class DiffView extends StatelessWidget {
  const DiffView({super.key, required this.diff});

  final DiffPreview diff;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Container(
      decoration: BoxDecoration(
        color: tokens.codeBg,
        borderRadius: BorderRadius.circular(tokens.radiusSm),
        border: Border.all(color: tokens.codeBorder),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: EdgeInsets.symmetric(
              horizontal: tokens.spaceSm,
              vertical: tokens.spaceXs,
            ),
            child: Row(
              children: [
                Icon(
                  Icons.description_outlined,
                  size: 14,
                  color: tokens.textMuted,
                ),
                SizedBox(width: tokens.spaceXs),
                Expanded(
                  child: Text(
                    diff.path,
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontFamily: 'monospace',
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                Text(
                  '+${diff.added}',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: tokens.success,
                  ),
                ),
                SizedBox(width: tokens.spaceXs),
                Text(
                  '−${diff.removed}',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: tokens.error,
                  ),
                ),
              ],
            ),
          ),
          Divider(height: 1, color: tokens.codeBorder),
          // The lines scroll sideways (a long line must not wrap) and every line's
          // background reaches the widest one: `IntrinsicWidth` is what gives the column
          // a bounded width inside a horizontal scroll view, which `stretch` alone
          // cannot do (it asks for an infinite one).
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: IntrinsicWidth(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  for (final line in diff.lines) _DiffLineTile(line: line),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _DiffLineTile extends StatelessWidget {
  const _DiffLineTile({required this.line});

  final DiffLine line;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final (marker, color) = switch (line.kind) {
      DiffLineKind.added => ('+', tokens.success),
      DiffLineKind.removed => ('−', tokens.error),
      DiffLineKind.context => (' ', tokens.textMuted),
    };
    return Container(
      color: switch (line.kind) {
        DiffLineKind.added => tokens.toolSuccessBg,
        DiffLineKind.removed => tokens.toolErrorBg,
        DiffLineKind.context => Colors.transparent,
      },
      padding: EdgeInsets.symmetric(horizontal: tokens.spaceSm, vertical: 1),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 14,
            child: Text(
              marker,
              style: TextStyle(
                fontFamily: 'monospace',
                fontSize: 12,
                color: color,
              ),
            ),
          ),
          SelectableText(
            line.text,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 12,
              height: 1.5,
              color: tokens.text,
            ),
          ),
        ],
      ),
    );
  }
}
