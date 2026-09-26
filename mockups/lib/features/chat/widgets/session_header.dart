import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/data/providers.dart';
import '../../../core/format.dart';
import '../../../core/models/scenario.dart';
import '../../../core/models/session.dart';
import '../../../core/theme/breakpoints.dart';
import '../../../core/theme/theme_tokens.dart';
import '../../../widgets/context_bar.dart';
import '../../../widgets/info_chip.dart';
import '../../../widgets/status_badge.dart';

/// The header of one session: identity, state, model, context and the actions
/// that act on the whole session.
class SessionHeader extends ConsumerWidget {
  const SessionHeader({super.key, required this.sessionId});

  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider(sessionId)).value;
    if (session == null) {
      return const SizedBox.shrink();
    }
    final tokens = context.tokens;
    final theme = Theme.of(context);
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
              IconButton(
                onPressed: () => showScenarioSheet(context, ref, sessionId),
                tooltip: 'Replay a scenario',
                icon: const Icon(Icons.play_circle_outline),
              ),
              _SessionMenu(session: session, sessionId: sessionId),
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
              if (session.pendingMessages > 0)
                InfoChip(
                  icon: Icons.queue,
                  label: '${session.pendingMessages} queued',
                  color: tokens.warning,
                ),
            ],
          ),
          SizedBox(height: tokens.spaceSm),
          ContextBar(
            usedTokens: session.contextTokens,
            windowTokens: session.contextWindow,
            costUsd: session.costUsd,
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
                  onPressed: () => ref.read(mockApiProvider).abort(sessionId),
                  child: const Text('Abort'),
                ),
              ],
            ),
          ],
          if (session.status == SessionStatus.crashed &&
              session.exitCode != null) ...[
            SizedBox(height: tokens.spaceXs),
            Text(
              'Child exited with code ${session.exitCode} · ${session.lastEventAt == null ? '' : relativeTime(session.lastEventAt!)}',
              style: theme.textTheme.labelSmall?.copyWith(color: tokens.error),
            ),
          ],
        ],
      ),
    );
  }
}

class _SessionMenu extends ConsumerWidget {
  const _SessionMenu({required this.session, required this.sessionId});

  final SessionModel session;
  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return PopupMenuButton<String>(
      tooltip: 'Session actions',
      icon: const Icon(Icons.more_vert),
      onSelected: (value) {
        final api = ref.read(mockApiProvider);
        switch (value) {
          case 'stop':
            api.stopSession(sessionId);
          case 'rename':
            api.renameSession(
              sessionId,
              'Renamed at ${TimeOfDay.now().format(context)}',
            );
        }
      },
      itemBuilder: (context) => [
        if (session.status.isLive)
          const PopupMenuItem(value: 'stop', child: Text('Stop session')),
        const PopupMenuItem(value: 'rename', child: Text('Rename…')),
        const PopupMenuItem(value: 'clone', child: Text('Clone')),
        const PopupMenuItem(value: 'export', child: Text('Export…')),
        const PopupMenuDivider(),
        const PopupMenuItem(
          value: 'remove',
          child: Text('Remove from list (keeps the JSONL)'),
        ),
      ],
    );
  }
}

/// Opens the scenario recorder for one session.
Future<void> showScenarioSheet(
  BuildContext context,
  WidgetRef ref,
  String sessionId,
) {
  return showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) => _ScenarioSheet(sessionId: sessionId),
  );
}

class _ScenarioSheet extends ConsumerWidget {
  const _ScenarioSheet({required this.sessionId});

  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tokens = context.tokens;
    return SafeArea(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxHeight: 520, maxWidth: 560),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: EdgeInsets.symmetric(horizontal: tokens.spaceLg),
              child: Text(
                'Replay a scenario',
                style: Theme.of(context).textTheme.titleMedium,
              ),
            ),
            Padding(
              padding: EdgeInsets.fromLTRB(
                tokens.spaceLg,
                tokens.spaceXs,
                tokens.spaceLg,
                tokens.spaceSm,
              ),
              child: Text(
                'Every state the plan must mock, driven through the same events the server sends.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
            Flexible(
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final scenario in MockScenario.values)
                    ListTile(
                      leading: Icon(scenario.icon),
                      title: Text(scenario.label),
                      subtitle: Text(scenario.description),
                      onTap: () {
                        ref
                            .read(mockApiProvider)
                            .runScenario(scenario, sessionId);
                        Navigator.of(context).pop();
                      },
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
