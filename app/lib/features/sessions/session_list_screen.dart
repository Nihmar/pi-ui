import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/providers.dart';
import '../../core/models/session.dart';
import '../../core/l10n/l10n.dart';
import '../../core/router.dart';
import '../../core/theme/breakpoints.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/connection_banner.dart';
import '../../widgets/empty_state.dart';
import 'widgets/new_session_sheet.dart';
import 'widgets/session_card.dart';

/// The sessions branch.
///
/// On a phone it is the list, full screen. From the desktop breakpoint it is the
/// master pane of a master/detail layout; the detail is the nested `:id` route.
class SessionListScreen extends ConsumerWidget {
  const SessionListScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(
        title: Text(context.l10n.sessionsTitle),
        actions: [
          IconButton(
            onPressed: () => ref.invalidate(sessionsProvider),
            tooltip: context.l10n.refresh,
            icon: const Icon(Icons.refresh),
          ),
          IconButton(
            onPressed: () => _createSession(context),
            tooltip: context.l10n.newSessionTooltip,
            icon: const Icon(Icons.add),
          ),
        ],
      ),
      body: Column(
        children: [
          const ConnectionBanner(),
          Expanded(
            child: context.isExpanded
                ? Row(
                    children: [
                      const SizedBox(width: 380, child: SessionListPane()),
                      const VerticalDivider(width: 1),
                      Expanded(
                        child: EmptyState(
                          icon: Icons.chat_bubble_outline,
                          title: context.l10n.selectASession,
                          message: context.l10n.pickASessionHint,
                        ),
                      ),
                    ],
                  )
                : const SessionListPane(),
          ),
        ],
      ),
    );
  }

  static Future<void> _createSession(BuildContext context) async {
    final sessionId = await showNewSessionSheet(context);
    if (sessionId != null && context.mounted) {
      context.go(Routes.chat(sessionId));
    }
  }
}

/// The list itself, without a scaffold: the phone branch and the desktop master
/// pane render the same widget.
class SessionListPane extends ConsumerWidget {
  const SessionListPane({super.key, this.selectedId});

  /// The session whose card is highlighted in the desktop layout.
  final String? selectedId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessions = ref.watch(sessionsProvider);
    if (sessions.hasError && !sessions.hasValue) {
      return _ListError(error: sessions.error);
    }
    final list = sessions.value ?? const <SessionModel>[];
    if (list.isEmpty) {
      return EmptyState(
        icon: Icons.forum_outlined,
        title: context.l10n.noSessionsTitle,
        message: context.l10n.noSessionsMessage,
      );
    }
    return ListView.builder(
      padding: EdgeInsets.symmetric(vertical: context.tokens.spaceSm),
      itemCount: list.length,
      itemBuilder: (context, index) {
        final session = list[index];
        return SessionCard(
          session: session,
          selected: session.id == selectedId,
          onTap: () => context.go(Routes.chat(session.id)),
          onStop: session.status.isLive
              ? () => _stop(context, ref, session)
              : null,
        );
      },
    );
  }

  static Future<void> _stop(
    BuildContext context,
    WidgetRef ref,
    SessionModel session,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.l10n.stopSessionTitle),
        content: Text(context.l10n.stopSessionMessage(session.cwd)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(context.l10n.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(context.l10n.stopSessionConfirm),
          ),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) {
      return;
    }
    final client = ref.read(clientProvider);
    if (client == null) {
      return;
    }
    try {
      await client.stopSession(session.id);
    } on Exception catch (error) {
      if (context.mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text('$error')));
      }
    }
  }
}

class _ListError extends ConsumerWidget {
  const _ListError({required this.error});

  final Object? error;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tokens = context.tokens;
    return Center(
      child: Padding(
        padding: EdgeInsets.all(tokens.spaceLg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.cloud_off, color: tokens.textDim),
            SizedBox(height: tokens.spaceSm),
            Text(
              context.l10n.sessionsFailedTitle,
              style: Theme.of(context).textTheme.titleSmall,
            ),
            SizedBox(height: tokens.spaceXs),
            Text(
              '$error',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodySmall,
            ),
            SizedBox(height: tokens.spaceMd),
            FilledButton(
              onPressed: () => ref.invalidate(sessionsProvider),
              child: Text(context.l10n.retry),
            ),
          ],
        ),
      ),
    );
  }
}
