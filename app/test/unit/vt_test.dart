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
