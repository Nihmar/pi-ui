import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/providers.dart';
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
  static const _commands = [
    ('/compact', 'Compact the context now'),
    ('/clear', 'Start a fresh context'),
    ('/model', 'Switch the model'),
    ('/skills', 'Run a skill'),
    ('/templates', 'Insert a prompt template'),
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
                        ? 'Reconnecting — the message waits, it is not lost'
                        : streaming
                        ? 'Steer the run or leave a follow-up'
                        : 'Message pi… (/ for commands)',
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
                        ? 'Connecting to the server…'
                        : 'Reconnecting: the message is sent when the link is back',
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ] else if (streaming) ...[
                Text('Session is streaming', style: theme.textTheme.labelSmall),
                SizedBox(width: tokens.spaceSm),
                Flexible(
                  child: Text(
                    effectiveMode == ComposerMode.steer
                        ? 'delivered after the current tool calls'
                        : 'delivered when the run settles',
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ] else
                Flexible(
                  child: Text(
                    'pi runs in the host directory shown above.',
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
  List<(String, String)> _slashMatches() {
    final text = _controller.text;
    if (!text.startsWith('/') || text.contains(' ')) {
      return const [];
    }
    return [
      for (final command in _commands)
        if (command.$1.startsWith(text)) command,
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
        label: const Text('Send'),
      );
    }

    final label = mode == ComposerMode.steer ? 'Steer' : 'Follow-up';
    // A phone has no room for the two-button segmented control: the primary
    // button sends with the current mode and the caret switches the mode.
    if (context.isCompact) {
      return MenuAnchor(
        menuChildren: [
          MenuItemButton(
            onPressed: () => onMode(ComposerMode.steer),
            leadingIcon: const Icon(Icons.bolt, size: 16),
            child: const Text('Steer'),
          ),
          MenuItemButton(
            onPressed: () => onMode(ComposerMode.followUp),
            leadingIcon: const Icon(Icons.subdirectory_arrow_right, size: 16),
            child: const Text('Follow-up'),
          ),
        ],
        builder: (context, controller, _) => Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            FilledButton(onPressed: onSend, child: Text(label)),
            IconButton(
              tooltip: 'Choose steer or follow-up',
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
          segments: const [
            ButtonSegment(value: ComposerMode.steer, label: Text('Steer')),
            ButtonSegment(
              value: ComposerMode.followUp,
              label: Text('Follow-up'),
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
          label: const Text('Queue'),
        ),
      ],
    );
  }
}

class _SlashMenu extends StatelessWidget {
  const _SlashMenu({required this.matches, required this.onPick});

  final List<(String, String)> matches;
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
              title: Text(command.$1),
              subtitle: Text(command.$2),
              onTap: () => onPick(command.$1),
            ),
        ],
      ),
    );
  }
}
