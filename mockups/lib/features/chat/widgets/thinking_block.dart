import 'package:flutter/material.dart';

import '../../../core/theme/theme_tokens.dart';

/// The collapsible reasoning block of an assistant message.
///
/// Collapsed by default: the answer is what the user reads, the thinking is what
/// they open when they want to know how it got there.
class ThinkingBlock extends StatefulWidget {
  const ThinkingBlock({super.key, required this.text});

  final String text;

  @override
  State<ThinkingBlock> createState() => _ThinkingBlockState();
}

class _ThinkingBlockState extends State<ThinkingBlock> {
  var _expanded = false;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        InkWell(
          onTap: () => setState(() => _expanded = !_expanded),
          borderRadius: BorderRadius.circular(tokens.radiusSm),
          child: Padding(
            padding: EdgeInsets.symmetric(vertical: tokens.spaceXs),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.psychology_outlined,
                  size: 14,
                  color: tokens.thinking,
                ),
                SizedBox(width: tokens.spaceXs),
                Text(
                  'Thinking',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: tokens.thinking,
                  ),
                ),
                SizedBox(width: tokens.spaceXs),
                Icon(
                  _expanded ? Icons.expand_less : Icons.expand_more,
                  size: 14,
                  color: tokens.thinking,
                ),
              ],
            ),
          ),
        ),
        if (_expanded)
          Container(
            margin: EdgeInsets.only(bottom: tokens.spaceSm),
            padding: EdgeInsets.all(tokens.spaceSm),
            decoration: BoxDecoration(
              color: tokens.surfaceAlt,
              borderRadius: BorderRadius.circular(tokens.radiusSm),
              border: Border.all(color: tokens.border),
            ),
            child: SelectableText(
              widget.text,
              style: theme.textTheme.bodySmall?.copyWith(
                color: tokens.thinking,
                fontStyle: FontStyle.italic,
                height: 1.45,
              ),
            ),
          ),
      ],
    );
  }
}
