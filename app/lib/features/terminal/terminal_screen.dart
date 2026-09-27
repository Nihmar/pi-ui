import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/frames.dart';
import '../../core/api/providers.dart';
import '../../core/api/socket.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';
import 'terminal_buffer.dart';

/// One PTY, as a screen: the shell's output, and a line to type into it.
///
/// It is deliberately not a full terminal emulator: it renders the byte stream in
/// monospace and sends what the user types, which is what a phone or a desktop window
/// needs for a build, a `git log` or a REPL. A VT parser is its own project.
class TerminalScreen extends ConsumerStatefulWidget {
  const TerminalScreen({super.key, required this.directory});

  /// The working directory of the shell; the server confines it to a workspace.
  final String directory;

  @override
  ConsumerState<TerminalScreen> createState() => _TerminalScreenState();
}

class _TerminalScreenState extends ConsumerState<TerminalScreen> {
  final _buffer = TerminalBuffer();
  final _input = TextEditingController();
  final _scroll = ScrollController();

  StreamSubscription<WsFrame>? _subscription;
  // The socket is kept in a field and not read from the container: `dispose` runs after
  // the widget is unmounted, and reading a provider there is what Riverpod forbids.
  PiUiSocket? _socket;
  String? _terminalId;
  String? _error;
  var _opening = true;
  var _closed = false;

  @override
  void initState() {
    super.initState();
    // The socket first, then the terminal: opening is a request, and its answer carries
    // the id every later frame needs.
    WidgetsBinding.instance.addPostFrameCallback((_) => _open());
  }

  @override
  void dispose() {
    _subscription?.cancel();
    final socket = _socket;
    final id = _terminalId;
    if (socket != null && id != null && !_closed) {
      // Closing is best effort: the server also closes a terminal whose connection is
      // gone, so a failed close is not something the user has to read.
      unawaited(socket.closeTerminal(id).catchError((Object _) {}));
    }
    _input.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _open() async {
    final socket = ref.read(socketProvider);
    _socket = socket;
    if (socket == null) {
      setState(() {
        _opening = false;
        _error = 'Not connected to the server.';
      });
      return;
    }
    _subscription = socket.terminals.listen(_onFrame);
    try {
      final answer = await socket.openTerminal(
        dir: widget.directory,
        cols: _columns(),
        rows: 32,
      );
      final id = answer?['terminalId'];
      if (!mounted) {
        return;
      }
      setState(() {
        _opening = false;
        _terminalId = id is String ? id : null;
        _error = id is String
            ? null
            : 'The server did not name the terminal it opened.';
      });
    } on PiuiException catch (error) {
      if (mounted) {
        setState(() {
          _opening = false;
          _error = error.message;
        });
      }
    }
  }

  /// The width in columns the pane can show, so the shell wraps where the user sees it.
  int _columns() {
    final width = MediaQuery.sizeOf(context).width;
    return (width / 8.5).floor().clamp(20, 240);
  }

  void _onFrame(WsFrame frame) {
    switch (frame) {
      case WsTerminalOutput(:final terminalId, :final bytes)
          when terminalId == _terminalId:
        setState(() => _buffer.append(bytes));
        _follow();
      case WsTerminalClosed(:final terminalId, :final exitCode, :final reason)
          when terminalId == _terminalId:
        setState(() {
          _closed = true;
          _buffer.append(utf8.encode('\n[closed: $reason, exit $exitCode]\n'));
        });
        _follow();
      default:
        break;
    }
  }

  /// Keeps the newest output in view: a terminal that scrolls away from what just
  /// happened is unreadable.
  void _follow() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) {
        _scroll.jumpTo(_scroll.position.maxScrollExtent);
      }
    });
  }

  Future<void> _send(String text) async {
    final socket = _socket;
    final id = _terminalId;
    if (socket == null || id == null || _closed) {
      return;
    }
    _input.clear();
    try {
      await socket.inputTerminal(id, utf8.encode('$text\n'));
    } on PiuiException catch (error) {
      if (mounted) {
        setState(() => _error = error.message);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    if (_error != null && _terminalId == null) {
      return Scaffold(
        appBar: AppBar(title: const Text('Terminal')),
        body: EmptyState(
          icon: Icons.terminal,
          title: 'No terminal',
          // The directory is still worth saying: it is where the shell would have run,
          // and a user who asked for a terminal there wants to know.
          message: '${_error!}\n\n${widget.directory}',
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.directory,
          style: theme.textTheme.titleSmall,
          overflow: TextOverflow.ellipsis,
        ),
        actions: [
          if (_closed)
            Padding(
              padding: EdgeInsets.symmetric(horizontal: tokens.spaceSm),
              child: Center(
                child: Text('closed', style: theme.textTheme.labelSmall),
              ),
            ),
          IconButton(
            tooltip: 'Clear the scrollback',
            onPressed: () => setState(_buffer.clear),
            icon: const Icon(Icons.cleaning_services_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          if (_buffer.isTruncated)
            Padding(
              padding: EdgeInsets.all(tokens.spaceXs),
              child: Text(
                'The oldest output was dropped.',
                style: theme.textTheme.labelSmall,
              ),
            ),
          Expanded(
            child: _opening
                ? const Center(child: CircularProgressIndicator())
                : SingleChildScrollView(
                    controller: _scroll,
                    padding: EdgeInsets.all(tokens.spaceMd),
                    child: SelectableText(
                      _buffer.text.isEmpty
                          ? 'The shell is ready. Type a command.'
                          : _buffer.text,
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12.5,
                        height: 1.35,
                        color: theme.colorScheme.onSurface,
                      ),
                    ),
                  ),
          ),
          // A line, not a keystroke stream: sending a command is what this screen is for,
          // and it is what an on-screen keyboard can do.
          Container(
            decoration: BoxDecoration(
              color: tokens.surface,
              border: Border(top: BorderSide(color: tokens.border)),
            ),
            padding: EdgeInsets.fromLTRB(
              tokens.spaceMd,
              tokens.spaceXs,
              tokens.spaceMd,
              tokens.spaceXs,
            ),
            child: Row(
              children: [
                Icon(Icons.chevron_right, size: 16, color: tokens.accent),
                SizedBox(width: tokens.spaceXs),
                Expanded(
                  child: TextField(
                    controller: _input,
                    enabled: !_closed && _terminalId != null,
                    style: const TextStyle(fontFamily: 'monospace'),
                    decoration: const InputDecoration(
                      border: InputBorder.none,
                      hintText: 'a command, then enter',
                    ),
                    onSubmitted: _send,
                  ),
                ),
                IconButton(
                  tooltip: 'Send',
                  onPressed: _closed ? null : () => _send(_input.text),
                  icon: const Icon(Icons.keyboard_return, size: 18),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
