import 'package:flutter/material.dart';
import 'package:piui_markdown/piui_markdown.dart';

import '../../../core/format.dart';
import '../../../core/models/chat_entry.dart';
import '../../../core/theme/markdown_theme.dart';
import '../../../core/theme/theme_tokens.dart';
import 'diff_view.dart';
import 'thinking_block.dart';

/// One timeline entry. The switch is the whole rendering contract: a new entry
/// kind is a new case, never a branch inside a bigger widget.
class EntryTile extends StatelessWidget {
  const EntryTile({super.key, required this.entry, this.onAction});

  final ChatEntry entry;

  /// What a tile's action button does (retry, open a path, expand…).
  final void Function(String action)? onAction;

  @override
  Widget build(BuildContext context) {
    // Bound locally so the switch promotes the sealed subtypes.
    final entry = this.entry;
    return switch (entry) {
      UserMessage() => _UserTile(entry: entry),
      AssistantMessage() => _AssistantTile(entry: entry),
      ToolCallEntry() => _ToolTile(entry: entry, onAction: onAction),
      StatusEntry() => _StatusTile(entry: entry),
      ErrorEntry() => _ErrorTile(entry: entry, onAction: onAction),
    };
  }
}

class _UserTile extends StatelessWidget {
  const _UserTile({required this.entry});

  final UserMessage entry;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Align(
      alignment: Alignment.centerRight,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: Container(
          padding: EdgeInsets.symmetric(
            horizontal: tokens.spaceMd,
            vertical: tokens.spaceSm,
          ),
          decoration: BoxDecoration(
            color: tokens.userBubble,
            borderRadius: BorderRadius.circular(tokens.radiusMd),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SelectableText(entry.text, style: theme.textTheme.bodyMedium),
              if (entry.queued) ...[
                SizedBox(height: tokens.spaceXs),
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.cloud_off_outlined,
                      size: 12,
                      color: tokens.warning,
                    ),
                    SizedBox(width: tokens.spaceXs),
                    Text(
                      'queued offline',
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: tokens.warning,
                      ),
                    ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _AssistantTile extends StatelessWidget {
  const _AssistantTile({required this.entry});

  final AssistantMessage entry;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (entry.thinking != null && entry.thinking!.isNotEmpty)
              ThinkingBlock(text: entry.thinking!),
            MarkdownView(
              entry.text,
              style: context.markdownStyle,
              onLinkTap: (url) => onTapLink(context, url),
            ),
            if (entry.streaming) ...[
              SizedBox(height: tokens.spaceXs),
              Text(
                '▍',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: tokens.accent,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ToolTile extends StatelessWidget {
  const _ToolTile({required this.entry, this.onAction});

  final ToolCallEntry entry;
  final void Function(String action)? onAction;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final background = switch (entry.status) {
      ToolStatus.pending || ToolStatus.running => tokens.toolPendingBg,
      ToolStatus.success => tokens.toolSuccessBg,
      ToolStatus.error || ToolStatus.denied => tokens.toolErrorBg,
    };
    final statusColor = switch (entry.status) {
      ToolStatus.pending || ToolStatus.running => tokens.warning,
      ToolStatus.success => tokens.success,
      ToolStatus.error || ToolStatus.denied => tokens.error,
    };
    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: Material(
          // Material owns the surface so the ExpansionTile's ink stays visible.
          color: background,
          clipBehavior: Clip.antiAlias,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(tokens.radiusMd),
            side: BorderSide(color: tokens.border),
          ),
          child: Theme(
            data: theme.copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              tilePadding: EdgeInsets.symmetric(horizontal: tokens.spaceMd),
              childrenPadding: EdgeInsets.fromLTRB(
                tokens.spaceMd,
                0,
                tokens.spaceMd,
                tokens.spaceMd,
              ),
              expandedCrossAxisAlignment: CrossAxisAlignment.stretch,
              leading: Icon(
                _iconFor(entry.name),
                size: 18,
                color: tokens.textMuted,
              ),
              title: Text(entry.title, style: theme.textTheme.bodyMedium),
              subtitle: entry.command == null
                  ? null
                  : Text(
                      entry.command!,
                      style: theme.textTheme.labelSmall?.copyWith(
                        fontFamily: 'monospace',
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (entry.duration != null) ...[
                    Text(
                      shortDuration(entry.duration!),
                      style: theme.textTheme.labelSmall,
                    ),
                    SizedBox(width: tokens.spaceSm),
                  ],
                  if (entry.status == ToolStatus.running ||
                      entry.status == ToolStatus.pending)
                    SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.5,
                        color: statusColor,
                      ),
                    )
                  else
                    Text(
                      entry.status.label,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: statusColor,
                      ),
                    ),
                  Icon(Icons.expand_more, size: 16, color: tokens.textDim),
                ],
              ),
              children: [
                if (entry.output != null)
                  Container(
                    width: double.infinity,
                    padding: EdgeInsets.all(tokens.spaceSm),
                    decoration: BoxDecoration(
                      color: tokens.codeBg,
                      borderRadius: BorderRadius.circular(tokens.radiusSm),
                      border: Border.all(color: tokens.codeBorder),
                    ),
                    child: SelectableText(
                      entry.output!,
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        height: 1.5,
                        color: tokens.text,
                      ),
                    ),
                  ),
                if (entry.diff != null) DiffView(diff: entry.diff!),
                if (entry.fullOutputPath != null) ...[
                  SizedBox(height: tokens.spaceSm),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: () => onAction?.call('open_full_output'),
                      icon: const Icon(Icons.open_in_new, size: 14),
                      label: Text(entry.fullOutputPath!),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  static IconData _iconFor(String name) => switch (name) {
    'bash' => Icons.terminal,
    'edit' || 'write' => Icons.edit_note,
    'read' => Icons.description_outlined,
    'search' || 'grep' => Icons.search,
    'web' => Icons.public,
    _ => Icons.build_outlined,
  };
}

class _StatusTile extends StatelessWidget {
  const _StatusTile({required this.entry});

  final StatusEntry entry;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final color = switch (entry.kind) {
      StatusKind.info => tokens.textMuted,
      StatusKind.success => tokens.success,
      StatusKind.warning => tokens.warning,
      StatusKind.error => tokens.error,
    };
    return Center(
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (entry.spinner) ...[
            SizedBox(
              width: 12,
              height: 12,
              child: CircularProgressIndicator(strokeWidth: 1.5, color: color),
            ),
            SizedBox(width: tokens.spaceSm),
          ] else
            Icon(Icons.circle, size: 6, color: color),
          SizedBox(width: tokens.spaceSm),
          Flexible(
            child: Text(
              entry.text,
              style: theme.textTheme.labelSmall?.copyWith(color: color),
              textAlign: TextAlign.center,
            ),
          ),
        ],
      ),
    );
  }
}

class _ErrorTile extends StatelessWidget {
  const _ErrorTile({required this.entry, this.onAction});

  final ErrorEntry entry;
  final void Function(String action)? onAction;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: Container(
          padding: EdgeInsets.all(tokens.spaceMd),
          decoration: BoxDecoration(
            color: tokens.toolErrorBg,
            borderRadius: BorderRadius.circular(tokens.radiusMd),
            border: Border.all(color: tokens.error.withValues(alpha: 0.5)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(Icons.error_outline, size: 16, color: tokens.error),
                  SizedBox(width: tokens.spaceSm),
                  Expanded(
                    child: Text(entry.title, style: theme.textTheme.titleSmall),
                  ),
                  if (entry.code != null)
                    Container(
                      padding: EdgeInsets.symmetric(
                        horizontal: tokens.spaceSm,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: tokens.surface,
                        borderRadius: BorderRadius.circular(tokens.radiusSm),
                        border: Border.all(color: tokens.border),
                      ),
                      child: Text(
                        entry.code!,
                        style: theme.textTheme.labelSmall?.copyWith(
                          fontFamily: 'monospace',
                        ),
                      ),
                    ),
                ],
              ),
              SizedBox(height: tokens.spaceSm),
              SelectableText(entry.message, style: theme.textTheme.bodySmall),
              if (entry.actionLabel != null) ...[
                SizedBox(height: tokens.spaceSm),
                Align(
                  alignment: Alignment.centerLeft,
                  child: FilledButton.tonal(
                    onPressed: () => onAction?.call('retry'),
                    child: Text(entry.actionLabel!),
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Opens a link tapped in markdown. External URLs are a platform call the real
/// client owns; the mockup shows the destination instead of leaving silently.
void onTapLink(BuildContext context, String url) {
  ScaffoldMessenger.of(context)
      .showSnackBar(SnackBar(content: Text('Open $url')));
}
