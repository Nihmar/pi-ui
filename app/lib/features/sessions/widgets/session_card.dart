import 'package:flutter/material.dart';

import '../../../core/format.dart';
import '../../../core/models/session.dart';
import '../../../core/l10n/l10n.dart';
import '../../../core/theme/theme_tokens.dart';
import '../../../widgets/context_bar.dart';
import '../../../widgets/info_chip.dart';
import '../../../widgets/status_badge.dart';

/// One session in the list: what it is, where it runs, how it is doing.
class SessionCard extends StatelessWidget {
  const SessionCard({
    super.key,
    required this.session,
    required this.selected,
    required this.onTap,
    this.onStop,
    this.onRemove,
  });

  final SessionModel session;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback? onStop;
  final VoidCallback? onRemove;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceMd,
        vertical: tokens.spaceXs,
      ),
      child: Material(
        color: selected ? tokens.selectedBg : tokens.surface,
        borderRadius: BorderRadius.circular(tokens.radiusMd),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(tokens.radiusMd),
          child: Container(
            padding: EdgeInsets.all(tokens.spaceMd),
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(tokens.radiusMd),
              border: Border.all(
                color: selected ? tokens.accent : tokens.border,
              ),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        session.displayName,
                        style: theme.textTheme.titleSmall,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    SizedBox(width: tokens.spaceSm),
                    StatusBadge(status: session.status, compact: true),
                    _menu(context),
                  ],
                ),
                SizedBox(height: tokens.spaceXs),
                Row(
                  children: [
                    Icon(
                      Icons.folder_outlined,
                      size: 14,
                      color: tokens.textDim,
                    ),
                    SizedBox(width: tokens.spaceXs),
                    Expanded(
                      child: Text(
                        session.shortenedCwd,
                        style: theme.textTheme.bodySmall,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
                SizedBox(height: tokens.spaceSm),
                Wrap(
                  spacing: tokens.spaceXs,
                  runSpacing: tokens.spaceXs,
                  children: [
                    if (session.provider != null || session.modelId != null)
                      InfoChip(
                        icon: Icons.memory,
                        label: [
                          session.provider,
                          session.modelId,
                        ].whereType<String>().join(' · '),
                      ),
                    if (session.thinkingLevel != null)
                      InfoChip(
                        icon: Icons.psychology_outlined,
                        label: session.thinkingLevel!,
                      ),
                  ],
                ),
                SizedBox(height: tokens.spaceMd),
                ContextBar(
                  usedTokens: session.contextTokens,
                  windowTokens: session.contextWindow,
                  costUsd: session.costUsd,
                  showLabels: false,
                ),
                SizedBox(height: tokens.spaceSm),
                Row(
                  children: [
                    Text(
                      session.lastEventAt == null
                          ? context.l10n.noEventsYet
                          : relativeTime(session.lastEventAt!),
                      style: theme.textTheme.labelSmall,
                    ),
                    const Spacer(),
                    Text(
                      context.l10n.messagesCount('${session.messageCount}'),
                      style: theme.textTheme.labelSmall,
                    ),
                    if (session.pendingMessages > 0) ...[
                      SizedBox(width: tokens.spaceSm),
                      Text(
                        context.l10n.queuedCount('${session.pendingMessages}'),
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: tokens.warning,
                        ),
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _menu(BuildContext context) {
    final items = <PopupMenuEntry<String>>[
      if (session.status.isLive)
        PopupMenuItem(value: 'stop', child: Text(context.l10n.stopSession)),
      PopupMenuItem(value: 'rename', child: Text(context.l10n.rename)),
      PopupMenuItem(value: 'clone', child: Text(context.l10n.clone)),
      PopupMenuItem(value: 'export', child: Text(context.l10n.export)),
      const PopupMenuDivider(),
      PopupMenuItem(value: 'remove', child: Text(context.l10n.removeFromList)),
    ];
    return PopupMenuButton<String>(
      tooltip: context.l10n.sessionActions,
      icon: Icon(Icons.more_vert, size: 18, color: context.tokens.textMuted),
      itemBuilder: (context) => items,
      onSelected: (value) {
        switch (value) {
          case 'stop':
            onStop?.call();
          case 'remove':
            onRemove?.call();
        }
      },
    );
  }
}
