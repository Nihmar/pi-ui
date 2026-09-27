import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/providers.dart';
import '../../../core/l10n/l10n.dart';
import '../../../core/api/socket.dart';
import '../../../core/models/session.dart';
import '../../../core/theme/breakpoints.dart';
import '../../../core/theme/theme_tokens.dart';

/// Where the next message goes.
enum ComposerMode {
  /// A normal prompt: rejected with `busy_streaming` while streaming.
  send,

  /// `session.steer`: delivered after the current turn's tool calls.
  steer,

  /// `session.follow_up`: delivered when the agent settles.
  followUp,
}

/// The message composer: the send/steer/follow-up choice, the slash shortcuts
/// and the offline behaviour (a send waits for the connection instead of
/// vanishing).
class Composer extends ConsumerStatefulWidget {
  const Composer({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<Composer> createState() => _ComposerState();
}

class _ComposerState extends ConsumerState<Composer> {
  final _controller = TextEditingController();
  final _focus = FocusNode();
  var _mode = ComposerMode.send;
  var _sending = false;

  /// The pi commands the slash menu offers. They are pi's own commands, not a
  /// client vocabulary: an unknown one is still accepted by `prompt`.
  /// The commands the slash menu offers, with their keys in the ARB: the list is built in
  /// `build` because a label depends on the locale.
  static const _commands = [
    '/compact',
    '/clear',
    '/model',
    '/skills',
    '/templates',
  ];

  @override
  void dispose() {
    _controller.dispose();
    _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final session = ref.watch(sessionProvider(widget.sessionId));
    final status = ref.watch(connectionProvider).value ?? SocketStatus.idle;
    final streaming = session?.status == SessionStatus.streaming;
    if (!streaming && _mode != ComposerMode.send) {
      // The run ended while the user was composing a steer: fall back to send.
      _mode = ComposerMode.send;
    }
    final matches = _slashMatches();
    // While the session streams, an untouched mode steers: a plain prompt would
    // come back as busy_streaming, and steering is what a user composing during
    // a run almost always means.
    final effectiveMode = streaming && _mode == ComposerMode.send
        ? ComposerMode.steer
        : _mode;
    final offline = !status.isOnline;

    return Container(
      decoration: BoxDecoration(
        color: tokens.surface,
        border: Border(top: BorderSide(color: tokens.border)),
      ),
      padding: EdgeInsets.fromLTRB(
        tokens.spaceLg,
        tokens.spaceSm,
        tokens.spaceLg,
        tokens.spaceMd,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (matches.isNotEmpty)
            _SlashMenu(
              matches: matches,
              onPick: (command) {
                _controller.text = '$command ';
                _controller.selection = TextSelection.collapsed(
                  offset: _controller.text.length,
                );
                _focus.requestFocus();
              },
            ),
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: TextField(
                  controller: _controller,
                  focusNode: _focus,
                  minLines: 1,
                  maxLines: 6,
                  decoration: InputDecoration(
                    hintText: offline
                        ? context.l10n.composerHintOffline
                        : streaming
                        ? context.l10n.composerHintStreaming
                        : context.l10n.composerHintIdle,
                  ),
                  onChanged: (_) => setState(() {}),
                ),
              ),
              SizedBox(width: tokens.spaceSm),
              _SendButton(
                streaming: streaming,
                mode: effectiveMode,
                busy: _sending,
                onMode: (mode) => setState(() => _mode = mode),
                onSend: _submit,
              ),
            ],
          ),
          SizedBox(height: tokens.spaceXs),
          Row(
            children: [
              if (offline) ...[
                Icon(Icons.cloud_off, size: 12, color: tokens.warning),
                SizedBox(width: tokens.spaceXs),
                Flexible(
                  child: Text(
                    status == SocketStatus.connecting
                        ? context.l10n.composerConnecting
                        : context.l10n.composerReconnecting,
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ] else if (streaming) ...[
                Text(
                  context.l10n.composerSteeringHint,
                  style: theme.textTheme.labelSmall,
                ),
                SizedBox(width: tokens.spaceSm),
                Flexible(
                  child: Text(
                    effectiveMode == ComposerMode.steer
                        ? context.l10n.composerSteerDetail
                        : context.l10n.composerFollowUpDetail,
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ] else
                Flexible(
                  child: Text(
                    context.l10n.composerCwdHint,
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }

  /// The slash suggestions for the current text, empty when not completing.
  List<String> _slashMatches() {
    final text = _controller.text;
    if (!text.startsWith('/') || text.contains(' ')) {
      return const [];
    }
    return [
      for (final command in _commands)
        if (command.startsWith(text)) command,
    ];
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    if (text.isEmpty || _sending) {
      return;
    }
    final actions = ref.read(sessionActionsProvider);
    if (actions == null) {
      return;
    }
    final streaming =
        ref.read(sessionProvider(widget.sessionId))?.status ==
        SessionStatus.streaming;
    final mode = streaming && _mode == ComposerMode.send
        ? ComposerMode.steer
        : _mode;
    setState(() => _sending = true);
    try {
      switch (mode) {
        case ComposerMode.steer:
          await actions.steer(widget.sessionId, text);
        case ComposerMode.followUp:
          await actions.followUp(widget.sessionId, text);
        case ComposerMode.send:
          await actions.prompt(widget.sessionId, text);
      }
      // The server accepted it: the entry arrives as an event, so there is no
      // optimistic bubble to reconcile.
      _controller.clear();
      _focus.requestFocus();
    } on PiuiException catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(error.message)));
      }
    } finally {
      if (mounted) {
        setState(() => _sending = false);
      }
    }
  }
}

class _SendButton extends StatelessWidget {
  const _SendButton({
    required this.streaming,
    required this.mode,
    required this.busy,
    required this.onMode,
    required this.onSend,
  });

  final bool streaming;
  final ComposerMode mode;
  final bool busy;
  final ValueChanged<ComposerMode> onMode;
  final VoidCallback onSend;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    if (busy) {
      return const Padding(
        padding: EdgeInsets.all(12),
        child: SizedBox(
          width: 18,
          height: 18,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    if (!streaming) {
      return FilledButton.icon(
        onPressed: onSend,
        icon: const Icon(Icons.arrow_upward, size: 16),
        label: Text(context.l10n.composerSend),
      );
    }

    final label = mode == ComposerMode.steer
        ? context.l10n.composerSteer
        : context.l10n.composerFollowUp;
    // A phone has no room for the two-button segmented control: the primary
    // button sends with the current mode and the caret switches the mode.
    if (context.isCompact) {
      return MenuAnchor(
        menuChildren: [
          MenuItemButton(
            onPressed: () => onMode(ComposerMode.steer),
            leadingIcon: const Icon(Icons.bolt, size: 16),
            child: Text(context.l10n.composerSteer),
          ),
          MenuItemButton(
            onPressed: () => onMode(ComposerMode.followUp),
            leadingIcon: const Icon(Icons.subdirectory_arrow_right, size: 16),
            child: Text(context.l10n.composerFollowUp),
          ),
        ],
        builder: (context, controller, _) => Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            FilledButton(onPressed: onSend, child: Text(label)),
            IconButton(
              tooltip: context.l10n.composerChooseMode,
              visualDensity: VisualDensity.compact,
              onPressed: () =>
                  controller.isOpen ? controller.close() : controller.open(),
              icon: const Icon(Icons.arrow_drop_down),
            ),
          ],
        ),
      );
    }

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        SegmentedButton<ComposerMode>(
          segments: [
            ButtonSegment(
              value: ComposerMode.steer,
              label: Text(context.l10n.composerSteer),
            ),
            ButtonSegment(
              value: ComposerMode.followUp,
              label: Text(context.l10n.composerFollowUp),
            ),
          ],
          selected: {mode},
          onSelectionChanged: (selection) => onMode(selection.first),
          showSelectedIcon: false,
          style: ButtonStyle(
            visualDensity: VisualDensity.compact,
            textStyle: WidgetStatePropertyAll(
              Theme.of(context).textTheme.labelSmall,
            ),
          ),
        ),
        SizedBox(width: tokens.spaceSm),
        FilledButton.icon(
          onPressed: onSend,
          icon: const Icon(Icons.arrow_upward, size: 16),
          label: Text(context.l10n.composerQueue),
        ),
      ],
    );
  }
}

/// The sentence of one slash command, translated.
String _describeCommand(BuildContext context, String command) =>
    switch (command) {
      '/compact' => context.l10n.slashCompact,
      '/clear' => context.l10n.slashClear,
      '/model' => context.l10n.slashModel,
      '/skills' => context.l10n.slashSkills,
      _ => context.l10n.slashTemplates,
    };

class _SlashMenu extends StatelessWidget {
  const _SlashMenu({required this.matches, required this.onPick});

  final List<String> matches;
  final ValueChanged<String> onPick;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    return Container(
      margin: EdgeInsets.only(bottom: tokens.spaceSm),
      decoration: BoxDecoration(
        color: tokens.surfaceAlt,
        borderRadius: BorderRadius.circular(tokens.radiusMd),
        border: Border.all(color: tokens.border),
      ),
      child: Column(
        children: [
          for (final command in matches)
            ListTile(
              dense: true,
              leading: const Icon(Icons.terminal, size: 16),
              title: Text(command),
              subtitle: Text(_describeCommand(context, command)),
              onTap: () => onPick(command),
            ),
        ],
      ),
    );
  }
}
