import 'package:flutter_test/flutter_test.dart';
import 'package:piui/features/terminal/vt.dart';

/// A screen the tests drive, small on purpose so a grid assertion is readable.
VtScreen screen({int columns = 10, int rows = 4, int scrollback = 100}) =>
    VtScreen(columns: columns, rows: rows, scrollbackLines: scrollback);

void main() {
  group('text', () {
    test('writes characters and advances', () {
      final vt = screen();
      vt.write('abc');

      expect(vt.charAt(0, 0), 'a');
      expect(vt.charAt(0, 1), 'b');
      expect(vt.charAt(0, 2), 'c');
      expect(vt.cursorRow, 0);
      expect(vt.cursorColumn, 3);
    });

    test('a newline moves down, a carriage return goes back', () {
      final vt = screen();
      vt.write('ab\r\ncd');

      expect(vt.lines[0].text, 'ab');
      expect(vt.lines[1].text, 'cd');
      expect(vt.cursorColumn, 2);
    });

    test('a long line wraps and the row says so', () {
      final vt = screen(columns: 5);
      vt.write('abcdefg');

      expect(vt.lines[0].text, 'abcde');
      expect(vt.lines[1].text, 'fg');
      expect(vt.cursorRow, 1);
      expect(vt.cursorColumn, 2);
    });

    test('a wide character takes two cells', () {
      final vt = screen();
      vt.write('日本');

      expect(vt.charAt(0, 0), '日');
      expect(vt.cellAt(0, 1).continuation, isTrue);
      expect(vt.charAt(0, 2), '本');
      expect(vt.cursorColumn, 4);
    });

    test('a tab jumps to the next multiple of eight', () {
      final vt = screen(columns: 20);
      vt.write('\tx');

      expect(vt.charAt(0, 8), 'x');
    });

    test('the bell is noticed and can be cleared', () {
      final vt = screen();
      vt.write('a\u0007');

      expect(vt.bell, isTrue);
      vt.clearBell();
      expect(vt.bell, isFalse);
    });
  });

  group('scrolling', () {
    test('the top line goes to the scrollback', () {
      final vt = screen(rows: 2);
      vt.write('one\r\ntwo\r\nthree');

      expect(vt.lines[0].text, 'two');
      expect(vt.lines[1].text, 'three');
      expect(vt.scrollback.single.text, 'one');
    });

    test('the scrollback is bounded', () {
      final vt = screen(rows: 2, scrollback: 2);
      vt.write('1\r\n2\r\n3\r\n4\r\n5');

      expect(vt.scrollback.length, 2);
      expect(vt.scrollback.first.text, '2');
      expect(vt.scrollback.last.text, '3');
    });

    test('CSI S and T scroll without moving the cursor', () {
      final vt = screen(rows: 3);
      vt.write('a\r\nb\r\nc');
      vt.write('\u001b[2S');

      expect(vt.lines[0].text, 'c');
      expect(vt.lines[1].text, '');
      expect(vt.lines[2].text, '');
    });
  });

  group('cursor and editing', () {
    test('CSI H positions the cursor, 1-based', () {
      final vt = screen();
      vt.write('\u001b[2;3Hx');

      expect(vt.charAt(1, 2), 'x');
      expect(vt.cursorRow, 1);
      expect(vt.cursorColumn, 3);
    });

    test('CSI A/B/C/D move relative', () {
      final vt = screen();
      vt.write('\u001b[3;3H');
      vt.write('\u001b[1A');
      expect(vt.cursorRow, 1);
      vt.write('\u001b[1B');
      expect(vt.cursorRow, 2);
      vt.write('\u001b[2D');
      expect(vt.cursorColumn, 0);
      vt.write('\u001b[2C');
      expect(vt.cursorColumn, 2);
    });

    test('CSI K erases the line from the cursor', () {
      final vt = screen();
      vt.write('abcdef');
      vt.write('\u001b[3G\u001b[K');

      expect(vt.lines[0].text, 'ab');
    });

    test('CSI 2K erases the whole line', () {
      final vt = screen();
      vt.write('abcdef');
      vt.write('\u001b[3G\u001b[2K');

      expect(vt.lines[0].text, '');
    });

    test('CSI J erases parts of the screen', () {
      final vt = screen(rows: 3);
      vt.write('aaa\r\nbbb\r\nccc');
      vt.write('\u001b[2;1H\u001b[J');

      expect(vt.lines[0].text, 'aaa');
      expect(vt.lines[1].text, '');
      expect(vt.lines[2].text, '');
    });

    test('CSI X erases characters without moving the rest', () {
      final vt = screen();
      vt.write('abcdef');
      vt.write('\u001b[3G\u001b[2X');

      expect(vt.lines[0].text, 'ab  ef');
    });

    test('CSI P deletes characters and shifts the rest left', () {
      final vt = screen();
      vt.write('abcdef');
      vt.write('\u001b[1G\u001b[2P');

      expect(vt.lines[0].text, 'cdef');
    });

    test('CSI L and M insert and delete lines inside the region', () {
      final vt = screen(rows: 3);
      vt.write('a\r\nb\r\nc');
      // Inserting a line pushes the others down; the one that falls out of the region is
      // gone, which is what a terminal does.
      vt.write('\u001b[2;1H\u001b[L');
      expect(vt.lines[0].text, 'a');
      expect(vt.lines[1].text, '');
      expect(vt.lines[2].text, 'b');

      // Deleting the empty line brings 'b' back up and leaves a blank at the bottom.
      vt.write('\u001b[2;1H\u001b[M');
      expect(vt.lines[0].text, 'a');
      expect(vt.lines[1].text, 'b');
      expect(vt.lines[2].text, '');
    });

    test('ESC 7 and ESC 8 save and restore the cursor', () {
      final vt = screen();
      vt.write('\u001b[2;4H\u001b7\u001b[1;1H\u001b8x');

      expect(vt.charAt(1, 3), 'x');
    });

    test('a program can hide the cursor', () {
      final vt = screen();
      vt.write('\u001b[?25l');
      expect(vt.cursorVisible, isFalse);
      vt.write('\u001b[?25h');
      expect(vt.cursorVisible, isTrue);
    });
  });

  group('attributes', () {
    test('SGR sets and clears colours', () {
      final vt = screen();
      vt.write('\u001b[31mred');
      expect(vt.cellAt(0, 0).foreground, 1);
      vt.write('\u001b[0mplain');
      expect(vt.cellAt(0, 3).foreground, isNull);
      expect(vt.cellAt(0, 3).char, 'p');
    });

    test('bright colours, bold and the reverse flag', () {
      final vt = screen();
      vt.write('\u001b[1;94;47mX');

      final cell = vt.cellAt(0, 0);
      expect(cell.bold, isTrue);
      expect(cell.foreground, 12);
      expect(cell.background, 7);
      vt.write('\u001b[7mY');
      expect(vt.cellAt(0, 1).inverse, isTrue);
      vt.write('\u001b[27mZ');
      expect(vt.cellAt(0, 2).inverse, isFalse);
    });

    test('256-colour and truecolour', () {
      final vt = screen();
      vt.write('\u001b[38;5;196mX');
      expect(vt.cellAt(0, 0).foreground, 196);
      vt.write('\u001b[48;2;10;20;30mY');
      final cell = vt.cellAt(0, 1);
      expect(cell.background, (10 << 16) | (20 << 8) | 30);
      expect(cell.trueColor, isTrue);
    });

    test('the palette knows the cube and the greys', () {
      expect(paletteColor(1), ansiPalette[1]);
      expect(paletteColor(16), 0x000000);
      expect(paletteColor(196), 0xff0000);
      expect(paletteColor(231), 0xffffff);
      expect(paletteColor(232), (8 << 16) | (8 << 8) | 8);
    });
  });

  group('screens and titles', () {
    test('the alternate screen hides the main one and restores it', () {
      final vt = screen();
      vt.write('main');
      vt.write('\u001b[?1049h');
      expect(vt.alternate, isTrue);
      expect(vt.lines[0].text, '');

      vt.write('full screen');
      expect(vt.lines[0].text, 'full scree');

      vt.write('\u001b[?1049l');
      expect(vt.alternate, isFalse);
      expect(vt.lines[0].text, 'main');
    });

    test('the scrollback does not grow while a full-screen program runs', () {
      final vt = screen(rows: 2);
      // One line scrolls off the main screen before the program starts.
      vt.write('a\r\nb\r\nc');
      expect(vt.scrollback.single.text, 'a');

      vt.write('\u001b[?1049h');
      vt.write('1\r\n2\r\n3\r\n4');
      vt.write('\u001b[?1049l');

      // The alternate screen's scrolling is not the reader's history.
      expect(vt.scrollback.length, 1);
      expect(vt.scrollback.single.text, 'a');
      expect(vt.lines[0].text, 'b');
      expect(vt.lines[1].text, 'c');
    });

    test('an OSC sequence sets the title', () {
      final vt = screen();
      vt.write('\u001b]0;pi-ui\u0007rest');

      expect(vt.title, 'pi-ui');
      expect(vt.lines[0].text, 'rest');
    });

    test('an OSC I do not implement is swallowed, not printed', () {
      final vt = screen();
      vt.write('\u001b]8;;https://example.com\u0007link');

      expect(vt.title, isNull);
      expect(vt.lines[0].text, 'link');
    });
  });

  _longTail();

  group('tolerance', () {
    test('an unknown CSI sequence changes nothing', () {
      final vt = screen();
      vt.write('abc');
      vt.write('\u001b[999z');

      expect(vt.lines[0].text, 'abc');
    });

    test('a bare escape at the end of a chunk does not lose the next one', () {
      final vt = screen();
      vt.write('a\u001b');
      vt.write('[31mb');

      expect(vt.cellAt(0, 1).foreground, 1);
    });

    test('resize keeps what is on screen and clamps the cursor', () {
      final vt = screen(columns: 10, rows: 4);
      vt.write('hello\r\nworld');
      vt.resize(3, 2);

      expect(vt.columns, 3);
      expect(vt.rows, 2);
      expect(vt.lines[0].text, 'hel');
      expect(vt.lines[1].text, 'wor');
      expect(vt.cursorRow, lessThan(2));
    });
  });
}

/// The long tail: the modes and encodings a full-screen program uses after the basics.
void _longTail() {
  group('DEC special graphics', () {
    test('a box drawn with letters comes out as a box', () {
      final vt = screen();
      vt.write('\u001b(0lqk');
      vt.write('\u001b(B');

      expect(vt.charAt(0, 0), '┌');
      expect(vt.charAt(0, 1), '─');
      expect(vt.charAt(0, 2), '┐');
    });

    test('the plain charset is restored by the selection, and by SI', () {
      final vt = screen();
      vt.write('\u001b(0q');
      expect(vt.charAt(0, 0), '─');
      vt.write('\u001b(Bq');
      expect(vt.charAt(0, 1), 'q');

      // SO selects G1, SI selects G0; both are a program's way of switching between them.
      vt.write('\u001b)0\u000eq');
      expect(vt.charAt(0, 2), '─');
      vt.write('\u000fq');
      expect(vt.charAt(0, 3), 'q');
    });

    test('an unselected graphics set leaves the letters alone', () {
      final vt = screen();
      vt.write('lqk');
      expect(vt.lines[0].text, 'lqk');
    });
  });

  group('modes', () {
    test('origin mode addresses the scroll region', () {
      final vt = screen(rows: 5);
      vt.write('\u001b[2;4r'); // region: rows 2-4

      vt.write('\u001b[1;1Hx');
      expect(
        vt.charAt(0, 0),
        'x',
        reason: 'without origin mode row 1 is the screen',
      );

      vt.write('\u001b[?6h');
      vt.write('\u001b[1;1Hy');
      expect(
        vt.charAt(1, 0),
        'y',
        reason: 'with origin mode row 1 is the region top',
      );
    });

    test('with auto-wrap off nothing wraps past the last column', () {
      final vt = screen(columns: 4, rows: 2);
      vt.write('\u001b[?7labcdefg');

      // Every character past the end lands on the last cell, overwriting the one before it:
      // no new line is started.
      expect(vt.lines[0].text, 'abcg');
      expect(vt.lines[1].text, '');
    });

    test('with auto-wrap on the same input wraps instead', () {
      final vt = screen(columns: 4, rows: 2);
      vt.write('abcdefg');

      expect(vt.lines[0].text, 'abcd');
      expect(vt.lines[1].text, 'efg');
    });
  });

  group('tabs', () {
    test('stops are every eight columns by default', () {
      final vt = screen(columns: 20);
      vt.write('\tx\ty');
      expect(vt.charAt(0, 8), 'x');
      expect(vt.charAt(0, 16), 'y');
    });

    test('a program can set, clear and step through its own stops', () {
      final vt = screen(columns: 20);
      vt.write('\u001b[3g'); // clear them all
      vt.write('\u001b[5G\u001bH'); // set one at column 5 (1-based)
      expect(vt.tabStops, contains(4), reason: 'HTS sets a stop at the cursor');
      vt.write('\u001b[1G\tx');
      expect(vt.charAt(0, 4), 'x');

      vt.write('\u001b[3g\u001b[1G\tz');
      expect(
        vt.charAt(0, 19),
        'z',
        reason: 'no stops left: the tab goes to the last column',
      );
    });

    test('CSI I and Z move without writing', () {
      final vt = screen(columns: 30);
      vt.write('\u001b[2Iy');
      expect(vt.charAt(0, 16), 'y');

      // Back to the nearest stop before the cursor, which is the one it just passed.
      vt.write('\u001b[1Zx');
      expect(vt.charAt(0, 16), 'x');
    });
  });

  group('screen alignment and cursor reports', () {
    test('DECALN fills the screen with E', () {
      final vt = screen(columns: 4, rows: 2);
      vt.write('\u001b#8');

      expect(vt.lines[0].text, 'EEEE');
      expect(vt.lines[1].text, 'EEEE');
    });

    test('a cursor position report answers with the position', () {
      final vt = screen();
      final replies = <String>[];
      vt.onResponse = replies.add;

      vt.write('\u001b[2;3H\u001b[6n');
      expect(replies, ['\u001b[2;3R']);

      vt.write('\u001b[5n');
      expect(replies.last, '\u001b[0n');

      vt.write('\u001b[c');
      expect(replies.last, contains('\u001b[?'));
    });

    test('a screen without a writer simply has no answer to give', () {
      final vt = screen();
      vt.write('\u001b[6n');
      expect(vt.cursorRow, 0);
    });
  });

  group('mouse reporting', () {
    test('nothing is reported unless a program asked', () {
      final vt = screen();
      expect(
        vt.encodeMouse(
          const MouseEvent(row: 0, column: 0, button: 0, action: 'press'),
        ),
        isNull,
      );
    });

    test('SGR reports press, release and moves with coordinates', () {
      final vt = screen();
      vt.write('\u001b[?1000h\u001b[?1006h');

      expect(
        vt.encodeMouse(
          const MouseEvent(row: 4, column: 9, button: 0, action: 'press'),
        ),
        '\u001b[<0;10;5M',
      );
      expect(
        vt.encodeMouse(
          const MouseEvent(row: 4, column: 9, button: 0, action: 'release'),
        ),
        '\u001b[<0;10;5m',
      );
      // A move is only reported when the program asked for drag or movement tracking.
      expect(
        vt.encodeMouse(
          const MouseEvent(row: 1, column: 1, button: 0, action: 'move'),
        ),
        isNull,
      );
      vt.write('\u001b[?1000l\u001b[?1002h');
      expect(
        vt.encodeMouse(
          const MouseEvent(row: 1, column: 1, button: 0, action: 'move'),
        ),
        '\u001b[<32;2;2M',
      );
    });

    test('the wheel and the modifier keys are part of the report', () {
      final vt = screen();
      vt.write('\u001b[?1000h\u001b[?1006h');

      expect(
        vt.encodeMouse(
          const MouseEvent(row: 0, column: 0, button: 64, action: 'press'),
        ),
        '\u001b[<64;1;1M',
      );
      expect(
        vt.encodeMouse(
          const MouseEvent(
            row: 0,
            column: 0,
            button: 0,
            action: 'press',
            ctrl: true,
            shift: true,
          ),
        ),
        '\u001b[<20;1;1M',
      );
    });

    test('the X10 encoding packs the position into bytes', () {
      final vt = screen();
      vt.write('\u001b[?1000h');

      final report = vt.encodeMouse(
        const MouseEvent(row: 2, column: 3, button: 0, action: 'press'),
      );
      expect(
        report,
        '\u001b[M${String.fromCharCode(32)}${String.fromCharCode(36)}${String.fromCharCode(35)}',
      );
      expect(
        vt.encodeMouse(
          const MouseEvent(row: 0, column: 0, button: 0, action: 'release'),
        ),
        '\u001b[M${String.fromCharCode(35)}${String.fromCharCode(33)}${String.fromCharCode(33)}',
      );
    });
  });
}
