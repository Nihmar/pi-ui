/// A terminal screen: the grid a full-screen program paints, the escape sequences that
/// paint it, and enough of the messy parts to be useful.
///
/// It is deliberately a *screen* and not a byte pipe. The plain buffer the terminal started
/// with renders a command's output; a REPL, a progress bar or `top` instead move the cursor,
/// erase a line and repaint it, and a byte pipe shows all of that as literal escape text.
///
/// What it implements: C0 controls, CSI cursor movement and editing, SGR (16 colours, 256
/// colours and truecolour), the scroll region, the alternate screen, OSC titles, DEC
/// save/restore, and scrollback. What it does not: DEC special graphics, double-width line
/// drawing characters beyond the common ones, and mouse reporting. An unknown sequence is
/// swallowed rather than printed, so an unsupported feature looks like a gap instead of
/// garbage.
library;

/// One cell of the grid.
class VtCell {
  const VtCell({
    this.char = ' ',
    this.foreground,
    this.background,
    this.bold = false,
    this.underline = false,
    this.italic = false,
    this.inverse = false,
    this.continuation = false,
  });

  /// The character in the cell (a space for an empty one).
  final String char;

  /// Foreground colour: 0–255 palette index, or a 24-bit RGB value when [trueColor] is set.
  final int? foreground;

  /// Background colour, same encoding.
  final int? background;

  final bool bold;
  final bool underline;
  final bool italic;
  final bool inverse;

  /// True for the second cell of a wide character: it draws nothing of its own.
  final bool continuation;

  /// True when the colour is an RGB value rather than a palette index.
  bool get trueColor => (foreground ?? 0) > 255 || (background ?? 0) > 255;

  /// The style flags, for comparing two cells in a test.
  bool sameStyle(VtCell other) =>
      foreground == other.foreground &&
      background == other.background &&
      bold == other.bold &&
      underline == other.underline &&
      italic == other.italic &&
      inverse == other.inverse;

  VtCell withChar(String value) => VtCell(
    char: value,
    foreground: foreground,
    background: background,
    bold: bold,
    underline: underline,
    italic: italic,
    inverse: inverse,
    continuation: continuation,
  );
}

/// One row of the screen, as the renderer reads it.
class VtLine {
  const VtLine(this.cells, {this.wrapped = false});

  final List<VtCell> cells;

  /// True when the row continues on the next one (a long line, not a new line).
  final bool wrapped;

  /// The text of the row, trailing blanks removed.
  String get text {
    final buffer = StringBuffer();
    for (final cell in cells) {
      if (cell.continuation) {
        continue;
      }
      buffer.write(cell.char);
    }
    return buffer.toString().replaceAll(RegExp(r'\s+$'), '');
  }
}

/// How a cell is drawn, as a palette index or an RGB value.
class VtColor {
  const VtColor.palette(int index) : value = index, isRgb = false;

  const VtColor.rgb(int rgb) : value = rgb, isRgb = true;

  final int value;
  final bool isRgb;

  @override
  bool operator ==(Object other) =>
      other is VtColor && other.value == value && other.isRgb == isRgb;

  @override
  int get hashCode => Object.hash(value, isRgb);
}

/// The DEC special graphics set: the characters a program writes to draw lines and boxes
/// after selecting it with `ESC ( 0`.
///
/// It is a plain table because it is a mapping and nothing else — `l` is a corner, `q` is a
/// horizontal line — and a terminal that ignores the selection prints `lqk` instead of a box,
/// which is what a shell prompt or `mc` looks like without it.
const Map<int, String> decSpecialGraphics = {
  0x60: '◆', // ` diamond
  0x61: '▒', // a checker board
  0x62: '␉', // b HT
  0x63: '␌', // c FF
  0x64: '␍', // d CR
  0x65: '␊', // e LF
  0x66: '°', // f degree
  0x67: '±', // g plus/minus
  0x68: '␤', // h NL
  0x69: '␋', // i VT
  0x6a: '┘', // j lower right corner
  0x6b: '┐', // k upper right corner
  0x6c: '┌', // l upper left corner
  0x6d: '└', // m lower left corner
  0x6e: '┼', // n crossing
  0x6f: '⎺', // o scan line 1
  0x70: '⎻', // p scan line 3
  0x71: '─', // q horizontal line
  0x72: '⎼', // r scan line 7
  0x73: '⎽', // s scan line 9
  0x74: '├', // t left tee
  0x75: '┤', // u right tee
  0x76: '┴', // v bottom tee
  0x77: '┬', // w top tee
  0x78: '│', // x vertical line
  0x79: '≤', // y less or equal
  0x7a: '≥', // z greater or equal
  0x7b: 'π', // { pi
  0x7c: '≠', // | not equal
  0x7d: '£', // } pound
  0x7e: '·', // ~ centred dot
};

/// Which of the two charsets is active, as the SO/SI controls switch them.
enum _Charset { ascii, graphics }

/// What a mouse report carries, which is what a program asked for.
enum MouseReport { none, click, drag, movement }

/// Where the mouse is and what it did.
class MouseEvent {
  const MouseEvent({
    required this.row,
    required this.column,
    required this.button,
    required this.action,
    this.shift = false,
    this.alt = false,
    this.ctrl = false,
  });

  /// 0-based cell the pointer is on.
  final int row;
  final int column;

  /// 0 left, 1 middle, 2 right, 64/65 wheel up/down.
  final int button;

  /// `press`, `release` or `move`.
  final String action;

  final bool shift;
  final bool alt;
  final bool ctrl;
}

/// The 16 ANSI colours, so a renderer does not have to know the palette.
const List<int> ansiPalette = [
  0x000000,
  0xcd0000,
  0x00cd00,
  0xcdcd00,
  0x0000ee,
  0xcd00cd,
  0x00cdcd,
  0xe5e5e5,
  0x7f7f7f,
  0xff0000,
  0x00ff00,
  0xffff00,
  0x5c5cff,
  0xff00ff,
  0x00ffff,
  0xffffff,
];

/// The six levels of the 6×6×6 colour cube, as xterm defines them. They are not evenly
/// spaced (`40 + 55×n` overflows into the next channel), which is why the table is written
/// out rather than computed.
const List<int> _cubeLevels = [0x00, 0x5f, 0x87, 0xaf, 0xd7, 0xff];

/// The colour of one palette index: the 16 ANSI colours, the 6×6×6 cube, then the greys.
int paletteColor(int index) {
  if (index < 0) {
    return 0;
  }
  if (index < 16) {
    return ansiPalette[index];
  }
  if (index < 232) {
    final cube = index - 16;
    return (_cubeLevels[cube ~/ 36] << 16) |
        (_cubeLevels[(cube % 36) ~/ 6] << 8) |
        _cubeLevels[cube % 6];
  }
  if (index < 256) {
    final grey = 8 + (index - 232) * 10;
    return (grey << 16) | (grey << 8) | grey;
  }
  // A truecolour value: the caller stored the RGB value itself.
  return index & 0xffffff;
}

/// Where the parser is.
enum _State { ground, escape, csi, osc, charset }

/// The screen of one terminal: a grid, a cursor, a scrollback and a parser.
class VtScreen {
  VtScreen({int columns = 80, int rows = 24, this.scrollbackLines = 2000})
    : _columns = columns.clamp(1, 500),
      _rows = rows.clamp(1, 500) {
    _resetGrid();
  }

  final int scrollbackLines;

  int _columns;
  int _rows;

  late List<List<VtCell>> _grid;

  /// Lines that scrolled off the top of the main screen.
  final List<VtLine> _scrollback = [];

  /// The main screen while the alternate one is active.
  List<List<VtCell>>? _savedGrid;
  int _savedCursorRow = 0;
  int _savedCursorColumn = 0;

  var _cursorRow = 0;
  var _cursorColumn = 0;
  var _savedRow = 0;
  var _savedColumn = 0;

  // The current attributes, applied to every character written.
  int? _foreground;
  int? _background;
  var _bold = false;
  var _underline = false;
  var _italic = false;
  var _inverse = false;

  var _cursorVisible = true;
  var _alternate = false;
  var _wrapNext = false;
  var _insertMode = false;
  var _autoWrap = true;
  var _originMode = false;
  _Charset _g0 = _Charset.ascii;
  _Charset _g1 = _Charset.ascii;
  _Charset get _charset => _activeCharset;
  _Charset _activeCharset = _Charset.ascii;

  /// Columns where a horizontal tab stops, 0-based. The default is every eight columns.
  late Set<int> _tabStops;

  /// Called with a reply a program asked for (`CSI 6n`, `CSI c`). A screen without a writer
  /// simply has no answer to give.
  void Function(String reply)? onResponse;

  MouseReport _mouseReport = MouseReport.none;
  var _mouseSgr = false;

  /// The scroll region, inclusive, 0-based.
  var _scrollTop = 0;
  late int _scrollBottom = _rows - 1;

  _State _state = _State.ground;
  int? _pendingCharset;
  final List<int> _params = [];
  int? _currentParam;
  final StringBuffer _osc = StringBuffer();

  /// The window title an OSC sequence set, when a program did.
  String? title;

  /// The bell rang since the last [clearBell].
  var bell = false;

  /// Columns of the screen.
  int get columns => _columns;

  /// Rows of the screen.
  int get rows => _rows;

  /// Where the cursor is, 0-based.
  int get cursorRow => _cursorRow;
  int get cursorColumn => _cursorColumn;

  /// True unless the program hid the cursor (`?25l`).
  bool get cursorVisible => _cursorVisible;

  /// True while the alternate screen is active (a full-screen program).
  bool get alternate => _alternate;

  /// What the program asked to be told about the mouse, if anything.
  MouseReport get mouseReport => _mouseReport;

  /// True when mouse reports use the SGR encoding (`CSI <b;x;yM`), which is the only one that
  /// survives coordinates past column 223.
  bool get mouseSgr => _mouseSgr;

  /// True while cursor addressing is relative to the scroll region.
  bool get originMode => _originMode;

  /// True while a character past the last column wraps to the next line.
  bool get autoWrap => _autoWrap;

  /// The columns a tab jumps to, for a test.
  Set<int> get tabStops => Set<int>.from(_tabStops);

  /// The lines scrolled off, oldest first.
  List<VtLine> get scrollback => [for (final line in _scrollback) line];

  /// The visible screen, with the cursor's row included.
  List<VtLine> get lines => [for (final row in _grid) VtLine(row)];

  /// The visible screen as text, for a test or a plain fallback.
  String get text {
    final buffer = StringBuffer();
    for (var index = 0; index < _grid.length; index++) {
      buffer.write(VtLine(_grid[index]).text);
      if (index < _grid.length - 1) {
        buffer.write('\n');
      }
    }
    return buffer.toString();
  }

  /// What is around a cell, for a test: the character at a position.
  String charAt(int row, int column) {
    if (row < 0 || row >= _rows || column < 0 || column >= _columns) {
      return '';
    }
    return _grid[row][column].char;
  }

  /// The attributes of a cell, for a test.
  VtCell cellAt(int row, int column) => _grid[row][column];

  /// Clears the bell.
  void clearBell() => bell = false;

  /// Feeds decoded text through the parser.
  void write(String data) {
    for (final rune in data.runes) {
      _feed(rune);
    }
  }

  /// Changes the size, keeping the top-left of what is on screen.
  void resize(int columns, int rows) {
    final newColumns = columns.clamp(1, 500);
    final newRows = rows.clamp(1, 500);
    if (newColumns == _columns && newRows == _rows) {
      return;
    }
    final resized = <List<VtCell>>[];
    for (var row = 0; row < newRows; row++) {
      final source = row < _grid.length ? _grid[row] : const <VtCell>[];
      resized.add([
        for (var column = 0; column < newColumns; column++)
          column < source.length ? source[column] : const VtCell(),
      ]);
    }
    _grid = resized;
    _columns = newColumns;
    _rows = newRows;
    _cursorRow = _cursorRow.clamp(0, _rows - 1);
    _cursorColumn = _cursorColumn.clamp(0, _columns - 1);
    _scrollTop = 0;
    _scrollBottom = _rows - 1;
    _wrapNext = false;
  }

  // --- the grid ---------------------------------------------------------------

  /// Builds an empty grid for the current size.
  void _resetGrid() {
    _grid = [
      for (var row = 0; row < _rows; row++)
        [for (var column = 0; column < _columns; column++) const VtCell()],
    ];
    _scrollTop = 0;
    _scrollBottom = _rows - 1;
    _tabStops = {for (var column = 8; column < _columns; column += 8) column};
  }

  /// A cell carrying the current attributes.
  VtCell _current() => VtCell(
    foreground: _foreground,
    background: _background,
    bold: _bold,
    underline: _underline,
    italic: _italic,
    inverse: _inverse,
  );

  /// True for a code point that occupies two cells.
  static bool _isWide(int value) {
    return (value >= 0x1100 && value <= 0x115f) ||
        (value >= 0x2e80 && value <= 0xa4cf) ||
        (value >= 0xac00 && value <= 0xd7a3) ||
        (value >= 0xf900 && value <= 0xfaff) ||
        (value >= 0xfe30 && value <= 0xfe6f) ||
        (value >= 0xff00 && value <= 0xff60) ||
        (value >= 0xffe0 && value <= 0xffe6) ||
        (value >= 0x1f300 && value <= 0x1f9ff);
  }

  /// Writes one character at the cursor and advances.
  void _put(int value) {
    // The active charset maps the character on the wire to the glyph on screen: DEC special
    // graphics is what draws a box without Unicode box-drawing bytes.
    final mapped = _charset == _Charset.graphics
        ? decSpecialGraphics[value]
        : null;
    final char = mapped ?? String.fromCharCode(value);
    if (_wrapNext) {
      // A program that wrote to the last column left the cursor pending: the next character
      // continues on a new line.
      _wrapNext = false;
      _cursorColumn = 0;
      _lineFeed();
    }
    if (_insertMode) {
      _grid[_cursorRow].insert(_cursorColumn, _current().withChar(char));
      _grid[_cursorRow].removeLast();
    }
    final wide = _isWide(value);
    _grid[_cursorRow][_cursorColumn] = _current().withChar(char);
    if (wide && _cursorColumn + 1 < _columns) {
      _grid[_cursorRow][_cursorColumn + 1] = _current().withChar(' ');
      _grid[_cursorRow][_cursorColumn + 1] = VtCell(
        foreground: _foreground,
        background: _background,
        bold: _bold,
        underline: _underline,
        italic: _italic,
        inverse: _inverse,
        continuation: true,
      );
    }
    final step = wide ? 2 : 1;
    if (_cursorColumn + step >= _columns) {
      _cursorColumn = _columns - 1;
      // With auto-wrap off (DECAWM reset) the cursor stays on the last column and the next
      // character overwrites it, which is what a program painting a status line wants.
      _wrapNext = _autoWrap;
      return;
    }
    _cursorColumn += step;
  }

  /// Moves down one line, scrolling the region when the cursor is at its bottom.
  void _lineFeed() {
    if (_cursorRow == _scrollBottom) {
      _scrollUp(1);
      return;
    }
    if (_cursorRow < _rows - 1) {
      _cursorRow++;
    }
  }

  /// Scrolls the region up by [count] lines, pushing the top line into the scrollback when
  /// the region is the whole screen.
  void _scrollUp(int count) {
    for (var step = 0; step < count; step++) {
      final removed = _grid[_scrollTop];
      if (_scrollTop == 0 && !_alternate) {
        _scrollback.add(VtLine(List<VtCell>.from(removed)));
        while (_scrollback.length > scrollbackLines) {
          _scrollback.removeAt(0);
        }
      }
      _grid.removeAt(_scrollTop);
      _grid.insert(_scrollBottom, [
        for (var column = 0; column < _columns; column++) const VtCell(),
      ]);
    }
  }

  /// Scrolls the region down by [count] lines.
  void _scrollDown(int count) {
    for (var step = 0; step < count; step++) {
      _grid.removeAt(_scrollBottom);
      _grid.insert(_scrollTop, [
        for (var column = 0; column < _columns; column++) const VtCell(),
      ]);
    }
  }

  /// Erases part of the current line.
  void _eraseLine(int mode) {
    final row = _grid[_cursorRow];
    switch (mode) {
      case 0:
        for (var column = _cursorColumn; column < _columns; column++) {
          row[column] = _current();
        }
      case 1:
        for (
          var column = 0;
          column <= _cursorColumn && column < _columns;
          column++
        ) {
          row[column] = _current();
        }
      default:
        for (var column = 0; column < _columns; column++) {
          row[column] = _current();
        }
    }
  }

  /// Erases part of the screen.
  void _eraseDisplay(int mode) {
    switch (mode) {
      case 0:
        _eraseLine(0);
        for (var row = _cursorRow + 1; row < _rows; row++) {
          _clearRow(row);
        }
      case 1:
        _eraseLine(1);
        for (var row = 0; row < _cursorRow; row++) {
          _clearRow(row);
        }
      case 2:
        for (var row = 0; row < _rows; row++) {
          _clearRow(row);
        }
      case 3:
        // Erase scrollback: the screen stays.
        _scrollback.clear();
    }
  }

  void _clearRow(int row) {
    for (var column = 0; column < _columns; column++) {
      _grid[row][column] = _current();
    }
  }

  // --- the parser -------------------------------------------------------------

  void _feed(int rune) {
    switch (_state) {
      case _State.ground:
        _ground(rune);
      case _State.escape:
        _escape(rune);
      case _State.csi:
        _csi(rune);
      case _State.osc:
        _oscFeed(rune);
      case _State.charset:
        _charsetByte(rune);
    }
  }

  /// One byte of a character-set selection (`ESC ( 0`) or of a `ESC #` sequence.
  void _charsetByte(int rune) {
    _state = _State.ground;
    final which = _pendingCharset;
    _pendingCharset = null;
    if (which != null) {
      final charset = rune == 0x30 ? _Charset.graphics : _Charset.ascii;
      if (which == 0) {
        _g0 = charset;
        _activeCharset = charset;
      } else if (which == 1) {
        _g1 = charset;
      }
      return;
    }
    // `ESC # 8`: the screen alignment test, which fills the screen with `E`.
    if (rune == 0x38) {
      for (final row in _grid) {
        for (var column = 0; column < _columns; column++) {
          row[column] = const VtCell(char: 'E');
        }
      }
    }
  }

  void _ground(int rune) {
    if (rune == 0x1b) {
      _state = _State.escape;
      return;
    }
    switch (rune) {
      case 0x07:
        bell = true;
      case 0x08:
        if (_cursorColumn > 0) {
          _cursorColumn--;
        }
        _wrapNext = false;
      case 0x09:
        // A tab goes to the next stop, which a program may have moved: the default of every
        // eight columns is only the starting point.
        _tabForward();
        _wrapNext = false;
      case 0x0a:
      case 0x0b:
      case 0x0c:
        _lineFeed();
        _wrapNext = false;
      case 0x0d:
        _cursorColumn = 0;
        _wrapNext = false;
      case 0x0e:
        _activeCharset = _g1;
      case 0x0f:
        _activeCharset = _g0;
      default:
        if (rune >= 0x20) {
          _put(rune);
        }
    }
  }

  void _escape(int rune) {
    _state = _State.ground;
    switch (rune) {
      case 0x5b: // [
        _state = _State.csi;
        _params.clear();
        _currentParam = null;
      case 0x5d: // ]
        _state = _State.osc;
        _osc.clear();
      case 0x28: // ( : select G0
        _pendingCharset = 0;
        _state = _State.charset;
      case 0x29: // ) : select G1
        _pendingCharset = 1;
        _state = _State.charset;
      case 0x2a: // *
      case 0x2b: // +
        _pendingCharset = -1;
        _state = _State.charset;
      case 0x23: // # : DECALN and friends
        _state = _State.charset;
      case 0x48: // H : horizontal tab set
        if (_cursorColumn < _columns) {
          _tabStops.add(_cursorColumn);
        }
      case 0x37: // 7: save cursor
        _savedRow = _cursorRow;
        _savedColumn = _cursorColumn;
      case 0x38: // 8: restore cursor
        _cursorRow = _savedRow.clamp(0, _rows - 1);
        _cursorColumn = _savedColumn.clamp(0, _columns - 1);
      case 0x44: // D: index (down)
        _lineFeed();
      case 0x45: // E: next line
        _cursorColumn = 0;
        _lineFeed();
      case 0x4d: // M: reverse index (up)
        if (_cursorRow == _scrollTop) {
          _scrollDown(1);
          return;
        }
        if (_cursorRow > 0) {
          _cursorRow--;
        }
      default:
        break;
    }
  }

  void _csi(int rune) {
    // Parameters: digits and `;`, with an optional private marker this screen accepts and
    // mostly ignores.
    if (rune >= 0x30 && rune <= 0x3f) {
      if (rune == 0x3b) {
        _params.add(_currentParam ?? 0);
        _currentParam = null;
        return;
      }
      if (rune >= 0x30 && rune <= 0x39) {
        _currentParam = (_currentParam ?? 0) * 10 + (rune - 0x30);
      } else {
        // `?`, `<`, `=`, `>`: a private sequence. The digit that follows still matters for
        // `?1049h`, so the marker itself is simply not stored.
        _currentParam ??= 0;
      }
      return;
    }
    if (rune >= 0x20 && rune <= 0x2f) {
      // Intermediate byte: part of a sequence this screen does not implement.
      return;
    }
    if (_params.isEmpty && _currentParam != null) {
      _params.add(_currentParam!);
    } else if (_currentParam != null) {
      _params.add(_currentParam!);
    }
    _currentParam = null;
    _state = _State.ground;

    int param(int index, [int fallback = 0]) =>
        index < _params.length && _params[index] != 0
        ? _params[index]
        : fallback;

    switch (rune) {
      case 0x40: // @ insert characters
        final count = param(0, 1);
        for (var step = 0; step < count; step++) {
          _grid[_cursorRow].insert(_cursorColumn, _current());
          _grid[_cursorRow].removeLast();
        }
      case 0x41: // A up
        _cursorRow = (_cursorRow - param(0, 1)).clamp(0, _rows - 1);
        _wrapNext = false;
      case 0x42: // B down
        _cursorRow = (_cursorRow + param(0, 1)).clamp(0, _rows - 1);
        _wrapNext = false;
      case 0x43: // C forward
        _cursorColumn = (_cursorColumn + param(0, 1)).clamp(0, _columns - 1);
        _wrapNext = false;
      case 0x44: // D back
        _cursorColumn = (_cursorColumn - param(0, 1)).clamp(0, _columns - 1);
        _wrapNext = false;
      case 0x45: // E next line
        _cursorRow = (_cursorRow + param(0, 1)).clamp(0, _rows - 1);
        _cursorColumn = 0;
      case 0x46: // F previous line
        _cursorRow = (_cursorRow - param(0, 1)).clamp(0, _rows - 1);
        _cursorColumn = 0;
      case 0x47: // G column
        _cursorColumn = (param(0, 1) - 1).clamp(0, _columns - 1);
        _wrapNext = false;
      case 0x48: // H position
      case 0x66: // f position
        // Origin mode (DECOM) makes row 1 the top of the scroll region, which is how a
        // full-screen program addresses its own window.
        final top = _originMode ? _scrollTop : 0;
        final bottom = _originMode ? _scrollBottom : _rows - 1;
        _cursorRow = (top + param(0, 1) - 1).clamp(top, bottom);
        _cursorColumn = (param(1, 1) - 1).clamp(0, _columns - 1);
        _wrapNext = false;
      case 0x4a: // J erase display
        _eraseDisplay(param(0));
      case 0x4b: // K erase line
        _eraseLine(param(0));
      case 0x4c: // L insert lines
        final count = param(0, 1);
        for (var step = 0; step < count; step++) {
          _grid.removeAt(_scrollBottom);
          _grid.insert(_cursorRow, [
            for (var column = 0; column < _columns; column++) const VtCell(),
          ]);
        }
      case 0x4d: // M delete lines
        final count = param(0, 1);
        for (var step = 0; step < count; step++) {
          _grid.removeAt(_cursorRow);
          _grid.insert(_scrollBottom, [
            for (var column = 0; column < _columns; column++) const VtCell(),
          ]);
        }
      case 0x50: // P delete characters
        final count = param(0, 1);
        for (var step = 0; step < count; step++) {
          _grid[_cursorRow].removeAt(_cursorColumn);
          _grid[_cursorRow].add(const VtCell());
        }
      case 0x53: // S scroll up
        _scrollUp(param(0, 1));
      case 0x54: // T scroll down
        _scrollDown(param(0, 1));
      case 0x58: // X erase characters
        final count = param(0, 1);
        for (
          var column = _cursorColumn;
          column < _cursorColumn + count && column < _columns;
          column++
        ) {
          _grid[_cursorRow][column] = _current();
        }
      case 0x49: // I: forward tab
        for (var step = 0; step < param(0, 1); step++) {
          _tabForward();
        }
      case 0x5a: // Z: backward tab
        for (var step = 0; step < param(0, 1); step++) {
          _tabBack();
        }
      case 0x67: // g: clear tab stops
        switch (param(0)) {
          case 3:
            _tabStops.clear();
          case 0:
            _tabStops.remove(_cursorColumn);
          default:
            // 4 clears every stop in a line; without per-line stops that is the same as 3.
            _tabStops.clear();
        }
      case 0x63: // c: device attributes
        onResponse?.call('\u001b[?62;1;2;4;6;9;15;22c');
      case 0x64: // d: line position
        _cursorRow = (param(0, 1) - 1).clamp(0, _rows - 1);
      case 0x68: // h set mode
        _setMode(true);
      case 0x6c: // l reset mode
        _setMode(false);
      case 0x6d: // m SGR
        _sgr();
      case 0x6e: // n: device status report
        if (param(0) == 6) {
          // The cursor position report, which a program may wait for: an editor uses it to
          // find out where it is before drawing.
          onResponse?.call('\u001b[${_cursorRow + 1};${_cursorColumn + 1}R');
        } else if (param(0) == 5) {
          onResponse?.call('\u001b[0n');
        }
      case 0x72: // r scroll region
        final top = (param(0, 1) - 1).clamp(0, _rows - 1);
        final bottom = (param(1, _rows) - 1).clamp(0, _rows - 1);
        if (top < bottom) {
          _scrollTop = top;
          _scrollBottom = bottom;
          _cursorRow = top;
          _cursorColumn = 0;
        }
      case 0x73: // s save cursor
        _savedRow = _cursorRow;
        _savedColumn = _cursorColumn;
      case 0x75: // u restore cursor
        _cursorRow = _savedRow.clamp(0, _rows - 1);
        _cursorColumn = _savedColumn.clamp(0, _columns - 1);
      default:
        break;
    }
  }

  void _oscFeed(int rune) {
    if (rune == 0x07) {
      _commitOsc();
      return;
    }
    if (rune == 0x1b) {
      // ST comes as ESC \: the next byte decides.
      _state = _State.escape;
      _commitOsc();
      return;
    }
    if (_osc.length < 256) {
      _osc.writeCharCode(rune);
    }
  }

  void _commitOsc() {
    _state = _State.ground;
    final text = _osc.toString();
    _osc.clear();
    if (text.startsWith('0;') || text.startsWith('2;')) {
      title = text.substring(2);
    }
  }

  /// The private modes this screen implements: the cursor, the alternate screen and
  /// bracketed paste (which it accepts and does not need to act on).
  void _setMode(bool enabled) {
    for (final mode in _params) {
      switch (mode) {
        case 25:
          _cursorVisible = enabled;
        case 1049:
          _switchAlternate(enabled);
        case 2004:
          break;
        case 4:
          _insertMode = enabled;
        case 6:
          _originMode = enabled;
          // A mode change moves the cursor home, relative to the region when origin mode is on.
          _cursorRow = _originMode ? _scrollTop : 0;
          _cursorColumn = 0;
        case 7:
          _autoWrap = enabled;
          _wrapNext = false;
        case 9:
          break; // X10 mouse: the oldest protocol, reported as clicks only
        case 1000:
          _mouseReport = enabled ? MouseReport.click : MouseReport.none;
        case 1002:
          _mouseReport = enabled ? MouseReport.drag : MouseReport.none;
        case 1003:
          _mouseReport = enabled ? MouseReport.movement : MouseReport.none;
        case 1006:
          _mouseSgr = enabled;
      }
    }
  }

  /// Switches to (and from) the alternate screen: a full-screen program's canvas, so
  /// quitting it restores what was on screen before.
  void _switchAlternate(bool enabled) {
    if (enabled == _alternate) {
      return;
    }
    if (enabled) {
      _savedGrid = _grid;
      _savedCursorRow = _cursorRow;
      _savedCursorColumn = _cursorColumn;
      _alternate = true;
      _resetGrid();
      _cursorRow = 0;
      _cursorColumn = 0;
    } else {
      _grid = _savedGrid ?? _grid;
      _savedGrid = null;
      _alternate = false;
      _cursorRow = _savedCursorRow.clamp(0, _grid.length - 1);
      _cursorColumn = _savedCursorColumn.clamp(0, _columns - 1);
    }
  }

  /// Moves to the next tab stop, or to the last column when there is none.
  void _tabForward() {
    for (var column = _cursorColumn + 1; column < _columns; column++) {
      if (_tabStops.contains(column)) {
        _cursorColumn = column;
        return;
      }
    }
    _cursorColumn = _columns - 1;
  }

  /// Moves to the previous tab stop, or to the first column.
  void _tabBack() {
    for (var column = _cursorColumn - 1; column >= 0; column--) {
      if (_tabStops.contains(column)) {
        _cursorColumn = column;
        return;
      }
    }
    _cursorColumn = 0;
  }

  /// Encodes one mouse event the way the program asked for it, or null when it did not.
  ///
  /// The SGR form (`CSI < b ; x ; y M/m`) is what a modern program requests and the only one
  /// that survives coordinates past 223; the X10 form is the original, which packs button and
  /// position into single bytes and reports a release as button 3.
  String? encodeMouse(MouseEvent event) {
    if (_mouseReport == MouseReport.none) {
      return null;
    }
    if (event.action == 'move' &&
        _mouseReport != MouseReport.drag &&
        _mouseReport != MouseReport.movement) {
      return null;
    }
    var modifiers = 0;
    if (event.shift) {
      modifiers |= 4;
    }
    if (event.alt) {
      modifiers |= 8;
    }
    if (event.ctrl) {
      modifiers |= 16;
    }
    if (_mouseSgr) {
      final terminator = event.action == 'release' ? 'm' : 'M';
      final button =
          event.button + modifiers + (event.action == 'move' ? 32 : 0);
      return '\u001b[<$button;${event.column + 1};${event.row + 1}$terminator';
    }
    // X10: `CSI M` then three bytes, each offset by 32. Coordinates are capped at 223, and a
    // release is reported as button 3 with the original button in the low bits.
    final button = event.action == 'release'
        ? 3 + modifiers
        : event.button + modifiers + (event.action == 'move' ? 32 : 0);
    final column = (event.column + 1).clamp(1, 223);
    final row = (event.row + 1).clamp(1, 223);
    return '\u001b[M${String.fromCharCode(32 + button)}'
        '${String.fromCharCode(32 + column)}${String.fromCharCode(32 + row)}';
  }

  /// Applies an SGR sequence: `ESC [ … m`.
  void _sgr() {
    if (_params.isEmpty) {
      _params.add(0);
    }
    for (var index = 0; index < _params.length; index++) {
      final code = _params[index];
      switch (code) {
        case 0:
          _foreground = null;
          _background = null;
          _bold = false;
          _underline = false;
          _italic = false;
          _inverse = false;
        case 1:
          _bold = true;
        case 3:
          _italic = true;
        case 4:
          _underline = true;
        case 7:
          _inverse = true;
        case 22:
          _bold = false;
        case 23:
          _italic = false;
        case 24:
          _underline = false;
        case 27:
          _inverse = false;
        case 39:
          _foreground = null;
        case 49:
          _background = null;
        default:
          if (code >= 30 && code <= 37) {
            _foreground = code - 30 + (_bold ? 8 : 0);
          } else if (code >= 40 && code <= 47) {
            _background = code - 40;
          } else if (code >= 90 && code <= 97) {
            _foreground = code - 90 + 8;
          } else if (code >= 100 && code <= 107) {
            _background = code - 100 + 8;
          } else if (code == 38 || code == 48) {
            final consumed = _extendedColor(index);
            index = consumed;
          }
      }
    }
    _params.clear();
  }

  /// Reads the `38`/`48` extended colour that follows, returning the last index it used.
  int _extendedColor(int index) {
    final target = _params[index];
    final mode = index + 1 < _params.length ? _params[index + 1] : null;
    if (mode == 5 && index + 2 < _params.length) {
      final color = _params[index + 2];
      if (target == 38) {
        _foreground = color;
      } else {
        _background = color;
      }
      return index + 2;
    }
    if (mode == 2 && index + 4 < _params.length) {
      final rgb =
          (_params[index + 2] << 16) |
          (_params[index + 3] << 8) |
          _params[index + 4];
      if (target == 38) {
        _foreground = rgb;
      } else {
        _background = rgb;
      }
      return index + 4;
    }
    return index;
  }
}
