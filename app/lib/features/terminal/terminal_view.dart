import 'package:flutter/material.dart';

import '../../core/theme/theme_tokens.dart';
import 'vt.dart';

/// A terminal screen, drawn.
///
/// The widget is a function of a [VtScreen]: it draws the grid, the colours and the cursor,
/// and it knows nothing about sockets, PTYs or where the bytes came from — which is what
/// makes the emulator testable on its own and the widget testable without a server.
class TerminalView extends StatelessWidget {
  const TerminalView({super.key, required this.screen, this.fontSize = 12.5});

  /// The screen to draw.
  final VtScreen screen;

  /// The monospace size; the column count already decided how wide the pane is.
  final double fontSize;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final defaultForeground = theme.colorScheme.onSurface;
    final defaultBackground = tokens.surface;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var row = 0; row < screen.lines.length; row++)
          _Line(
            line: screen.lines[row],
            cursorColumn: screen.cursorVisible && screen.cursorRow == row
                ? screen.cursorColumn
                : null,
            defaultForeground: defaultForeground,
            defaultBackground: defaultBackground,
            fontSize: fontSize,
          ),
      ],
    );
  }
}

/// One row, drawn as runs of equal style so a coloured prompt is one span per colour rather
/// than one per character.
class _Line extends StatelessWidget {
  const _Line({
    required this.line,
    required this.cursorColumn,
    required this.defaultForeground,
    required this.defaultBackground,
    required this.fontSize,
  });

  final VtLine line;
  final int? cursorColumn;
  final Color defaultForeground;
  final Color defaultBackground;
  final double fontSize;

  @override
  Widget build(BuildContext context) {
    final spans = <TextSpan>[];
    var run = StringBuffer();
    VtCell? runStyle;

    void flush() {
      final style = runStyle;
      if (run.isEmpty || style == null) {
        run = StringBuffer();
        return;
      }
      spans.add(TextSpan(text: run.toString(), style: _style(style)));
      run = StringBuffer();
    }

    for (var column = 0; column < line.cells.length; column++) {
      final cell = line.cells[column];
      if (cell.continuation) {
        continue;
      }
      final highlight = cursorColumn == column;
      final style = runStyle;
      if (style == null || !style.sameStyle(cell) || highlight) {
        flush();
        runStyle = cell;
      }
      if (highlight) {
        // The cursor is the cell drawn on the cursor's own background: a block, which is
        // what a terminal shows when nothing else is selected.
        spans.add(TextSpan(text: cell.char, style: _style(cell, cursor: true)));
        runStyle = null;
        continue;
      }
      run.write(cell.char);
    }
    flush();

    return SizedBox(
      height: fontSize * 1.35,
      child: RichText(
        maxLines: 1,
        text: TextSpan(
          style: TextStyle(
            fontFamily: 'monospace',
            fontSize: fontSize,
            color: defaultForeground,
          ),
          children: spans,
        ),
      ),
    );
  }

  /// The colour of one cell: the palette value, the reverse flag applied, and the defaults
  /// where the program said nothing.
  TextStyle _style(VtCell cell, {bool cursor = false}) {
    var foreground = cell.foreground == null
        ? defaultForeground
        : Color(
            0xff000000 |
                (cell.foreground! > 255
                    ? cell.foreground!
                    : paletteColor(cell.foreground!)),
          );
    // A cell with no background is not painted: filling every span with the surface colour
    // would repaint the whole row and hide the pane behind it.
    Color? background = cell.background == null
        ? null
        : Color(
            0xff000000 |
                (cell.background! > 255
                    ? cell.background!
                    : paletteColor(cell.background!)),
          );
    if (cell.inverse) {
      // The inverse of "no background" is the terminal's own background, and the inverse of
      // no foreground is its own text colour.
      final swap = foreground;
      foreground = background ?? defaultBackground;
      background = swap;
    }
    if (cursor) {
      final swap = foreground;
      foreground = background ?? defaultBackground;
      background = swap;
    }
    return TextStyle(
      fontFamily: 'monospace',
      fontSize: fontSize,
      height: 1.35,
      color: foreground,
      backgroundColor: background,
      fontWeight: cell.bold ? FontWeight.w700 : FontWeight.w400,
      fontStyle: cell.italic ? FontStyle.italic : FontStyle.normal,
      decoration: cell.underline
          ? TextDecoration.underline
          : TextDecoration.none,
    );
  }
}
