import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/providers.dart';
import '../../../core/api/session_stats.dart';
import '../../../core/format.dart';
import '../../../core/models/session.dart';
import '../../../core/theme/breakpoints.dart';
import '../../../core/theme/theme_tokens.dart';
import '../../../widgets/context_bar.dart';
import '../../../widgets/info_chip.dart';
import '../../../widgets/status_badge.dart';
import '../../git/git_panel.dart';
import 'model_picker.dart';

/// The header of one session: identity, state, model, context and the actions
/// that act on the whole session.
class SessionHeader extends ConsumerWidget {
  const SessionHeader({super.key, required this.sessionId, this.stats});

  final String sessionId;

  /// The numbers the child reported (`get_state`, `get_session_stats`).
  final SessionStats? stats;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider(sessionId));
    if (session == null) {
      return const SizedBox.shrink();
    }
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final metrics = (stats ?? const SessionStats()).merge(
      SessionStats(
        messageCount: session.messageCount,
        pendingMessages: session.pendingMessages,
        provider: session.provider,
        modelId: session.modelId,
        thinkingLevel: session.thinkingLevel,
      ),
    );
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: tokens.spaceLg,
        vertical: tokens.spaceMd,
      ),
      decoration: BoxDecoration(
        color: tokens.surface,
        border: Border(bottom: BorderSide(color: tokens.border)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (context.isCompact && Navigator.of(context).canPop()) ...[
                IconButton(
                  onPressed: () => Navigator.of(context).maybePop(),
                  tooltip: 'Back',
                  icon: const Icon(Icons.arrow_back),
                ),
                SizedBox(width: tokens.spaceXs),
              ],
              Flexible(
                child: Text(
                  session.displayName,
                  style: theme.textTheme.titleMedium,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              SizedBox(width: tokens.spaceSm),
              StatusBadge(status: session.status),
              const Spacer(),
              _SessionMenu(session: session),
            ],
          ),
          SizedBox(height: tokens.spaceXs),
          Row(
            children: [
              Icon(Icons.folder_outlined, size: 14, color: tokens.textDim),
              SizedBox(width: tokens.spaceXs),
              Expanded(
                child: Text(
                  session.cwd,
                  style: theme.textTheme.bodySmall,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          SizedBox(height: tokens.spaceSm),
          Wrap(
            spacing: tokens.spaceSm,
            runSpacing: tokens.spaceXs,
            children: [
              // The model chip opens the picker: what pi offers is a question for pi,
              // and the answer goes back through the same command passthrough.
              if (metrics.provider != null || metrics.modelId != null)
                InkWell(
                  onTap: () => showModelPicker(context, ref, sessionId),
                  borderRadius: BorderRadius.circular(tokens.radiusLg),
                  child: InfoChip(
                    icon: Icons.memory,
                    label: [
                      metrics.provider,
                      metrics.modelId,
                    ].whereType<String>().join(' · '),
                  ),
                ),
              if (metrics.thinkingLevel != null)
                InfoChip(
                  icon: Icons.psychology_outlined,
                  label: metrics.thinkingLevel!,
                ),
              if (metrics.pendingMessages > 0)
                InfoChip(
                  icon: Icons.queue,
                  label: '${metrics.pendingMessages} queued',
                  color: tokens.warning,
                ),
            ],
          ),
          SizedBox(height: tokens.spaceSm),
          ContextBar(
            usedTokens: metrics.contextTokens,
            windowTokens: metrics.contextWindow,
            costUsd: metrics.costUsd,
          ),
          if (session.status == SessionStatus.streaming) ...[
            SizedBox(height: tokens.spaceSm),
            Row(
              children: [
                SizedBox(
                  width: 12,
                  height: 12,
                  child: CircularProgressIndicator(
                    strokeWidth: 1.5,
                    color: tokens.accent,
                  ),
                ),
                SizedBox(width: tokens.spaceSm),
                Text(
                  'The agent is working…',
                  style: theme.textTheme.labelSmall,
                ),
                const Spacer(),
                TextButton(
                  onPressed: () => _abort(context, ref, sessionId),
                  child: const Text('Abort'),
                ),
              ],
            ),
          ],
          if (session.status == SessionStatus.crashed &&
              session.exitCode != null) ...[
            SizedBox(height: tokens.spaceXs),
            Text(
              'Child exited with code ${session.exitCode} · '
              '${session.lastEventAt == null ? '' : relativeTime(session.lastEventAt!)}',
              style: theme.textTheme.labelSmall?.copyWith(color: tokens.error),
            ),
          ],
        ],
      ),
    );
  }

  static Future<void> _abort(
    BuildContext context,
    WidgetRef ref,
    String sessionId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(sessionActionsProvider)?.abort(sessionId);
    } on PiuiException catch (error) {
      messenger.showSnackBar(SnackBar(content: Text(error.message)));
    }
  }
}

class _SessionMenu extends ConsumerWidget {
  const _SessionMenu({required this.session});

  final SessionModel session;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return PopupMenuButton<String>(
      tooltip: 'Session actions',
      icon: const Icon(Icons.more_vert),
      onSelected: (value) => _onSelected(context, ref, value),
      itemBuilder: (context) => [
        if (session.status.isLive)
          const PopupMenuItem(value: 'stop', child: Text('Stop session')),
        const PopupMenuItem(value: 'rename', child: Text('Rename…')),
        const PopupMenuItem(value: 'git', child: Text('Git…')),
        const PopupMenuItem(value: 'compact', child: Text('Compact context')),
        const PopupMenuDivider(),
        const PopupMenuItem(
          value: 'copy-cwd',
          child: Text('Copy the working directory'),
        ),
      ],
    );
  }

  Future<void> _onSelected(
    BuildContext context,
    WidgetRef ref,
    String action,
  ) async {
    final actions = ref.read(sessionActionsProvider);
    final messenger = ScaffoldMessenger.of(context);
    if (actions == null) {
      return;
    }
    try {
      switch (action) {
        case 'stop':
          await actions.stop(session.id);
        case 'git':
          if (context.mounted) {
            unawaited(showGitPanel(context, session.cwd));
          }
        case 'compact':
          await actions.compact(session.id);
        case 'copy-cwd':
          // No clipboard dependency yet: the cwd is shown in the header.
          messenger.showSnackBar(SnackBar(content: Text(session.cwd)));
        case 'rename':
          final name = await _askName(context, session.displayName);
          if (name != null && name.trim().isNotEmpty) {
            await actions.rename(session.id, name.trim());
          }
      }
    } on PiuiException catch (error) {
      messenger.showSnackBar(SnackBar(content: Text(error.message)));
    }
  }

  Future<String?> _askName(BuildContext context, String current) async {
    final controller = TextEditingController(text: current);
    try {
      return await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('Rename the session'),
          content: TextField(
            controller: controller,
            autofocus: true,
            decoration: const InputDecoration(labelText: 'Name'),
            onSubmitted: (value) => Navigator.of(context).pop(value),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(context).pop(controller.text),
              child: const Text('Rename'),
            ),
          ],
        ),
      );
    } finally {
      controller.dispose();
    }
  }
}
