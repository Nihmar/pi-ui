import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/frames.dart';
import '../../core/api/providers.dart';
import '../../core/api/socket.dart';
import '../../core/l10n/l10n.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';
import 'terminal_view.dart';
import 'utf8_stream.dart';
import 'vt.dart';

/// One PTY, as a screen: the shell's output, painted by the emulator, and a line to type
/// into it.
///
/// The emulator (see `vt.dart`) is what makes this a terminal rather than a log: a REPL, a
/// progress bar or `top` moves the cursor and repaints, and the grid follows.
class TerminalScreen extends ConsumerStatefulWidget {
  const TerminalScreen({super.key, required this.directory});

  /// The working directory of the shell; the server confines it to a workspace.
  final String directory;

  @override
  ConsumerState<TerminalScreen> createState() => _TerminalScreenState();
}

class _TerminalScreenState extends ConsumerState<TerminalScreen> {
  /// The screen the shell paints, and the decoder that feeds it: the emulator owns the
  /// scrollback and the cursor, the stream keeps a split character whole.
  final _screen = VtScreen();
  final _bytes = Utf8Stream();
  final _input = TextEditingController();
  final _scroll = ScrollController();

  StreamSubscription<WsFrame>? _subscription;

  // The socket is kept in a field and not read from the container: `dispose` runs after the
  // widget is unmounted, and reading a provider there is what Riverpod forbids.
  PiUiSocket? _socket;
  String? _terminalId;
  String? _error;
  var _opening = true;
  var _closed = false;

  /// Roughly the width of one monospace cell at the size the view draws.
  static const _cellWidth = 7.9;
  static const _fontSize = 12.5;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _open());
  }

  @override
  void dispose() {
    _subscription?.cancel();
    final socket = _socket;
    final id = _terminalId;
    if (socket != null && id != null && !_closed) {
      // Closing is best effort: the server also closes a terminal whose connection is gone.
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
        _error = context.l10n.chatNotConnected;
      });
      return;
    }
    _subscription = socket.terminals.listen(_onFrame);
    try {
      final size = _paneSize();
      _screen.resize(size.columns, size.rows);
      final answer = await socket.openTerminal(
        dir: widget.directory,
        cols: size.columns,
        rows: size.rows,
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

  /// The grid the pane can show, from its size: a shell told the wrong size wraps in the
  /// wrong place.
  ({int columns, int rows}) _paneSize() {
    final size = MediaQuery.sizeOf(context);
    return (
      columns: (size.width / _cellWidth).floor().clamp(20, 240),
      rows: ((size.height - 160) / (_fontSize * 1.35)).floor().clamp(5, 200),
    );
  }

  void _onFrame(WsFrame frame) {
    switch (frame) {
      case WsTerminalOutput(:final terminalId, :final bytes)
          when terminalId == _terminalId:
        final text = _bytes.push(bytes);
        setState(() => _screen.write(text));
        _follow();
      case WsTerminalClosed(:final terminalId, :final exitCode, :final reason)
          when terminalId == _terminalId:
        setState(() {
          _closed = true;
          _screen.write('\r\n[closed: $reason, exit $exitCode]\r\n');
        });
        _follow();
      default:
        break;
    }
  }

  /// Keeps the newest output in view: a terminal that scrolls away from what just happened
  /// is unreadable.
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
        appBar: AppBar(title: Text(context.l10n.terminalTitle)),
        body: EmptyState(
          icon: Icons.terminal,
          title: context.l10n.terminalNoTerminal,
          // The directory is still worth saying: it is where the shell would have run.
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
                child: Text(
                  context.l10n.terminalClosed,
                  style: theme.textTheme.labelSmall,
                ),
              ),
            ),
          IconButton(
            tooltip: context.l10n.terminalClear,
            // Clearing is a scrollback reset and nothing else: the shell keeps its state.
            onPressed: () =>
                setState(() => _screen.write('\u001b[3J\u001b[2J\u001b[H')),
            icon: const Icon(Icons.cleaning_services_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: _opening
                ? const Center(child: CircularProgressIndicator())
                : LayoutBuilder(
                    builder: (context, constraints) {
                      final columns = (constraints.maxWidth / _cellWidth)
                          .floor()
                          .clamp(20, 240);
                      final rows = (constraints.maxHeight / (_fontSize * 1.35))
                          .floor()
                          .clamp(5, 200);
                      if (columns != _screen.columns || rows != _screen.rows) {
                        _screen.resize(columns, rows);
                        final id = _terminalId;
                        if (id != null && !_closed) {
                          unawaited(
                            _socket
                                    ?.resizeTerminal(id, columns, rows)
                                    .catchError((Object _) {}) ??
                                Future<void>.value(),
                          );
                        }
                      }
                      return SingleChildScrollView(
                        controller: _scroll,
                        padding: EdgeInsets.all(tokens.spaceMd),
                        child: TerminalView(
                          screen: _screen,
                          fontSize: _fontSize,
                        ),
                      );
                    },
                  ),
          ),
          // A line, not a keystroke stream: sending a command is what this screen is for, and
          // it is what an on-screen keyboard can do.
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
                    decoration: InputDecoration(
                      border: InputBorder.none,
                      hintText: context.l10n.terminalHint,
                    ),
                    onSubmitted: _send,
                  ),
                ),
                IconButton(
                  tooltip: context.l10n.terminalSend,
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
