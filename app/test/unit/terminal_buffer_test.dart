import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/features/terminal/terminal_buffer.dart';

void main() {
  group('TerminalBuffer', () {
    test('accumulates chunks in order', () {
      final buffer = TerminalBuffer();
      buffer.append(utf8.encode('one\n'));
      buffer.append(utf8.encode('two\n'));

      expect(buffer.text, 'one\ntwo\n');
      expect(buffer.length, 8);
      expect(buffer.isTruncated, isFalse);
    });

    test('a rune split across two chunks survives', () {
      final buffer = TerminalBuffer();
      // "è" is two bytes; a terminal read can end between them.
      final bytes = utf8.encode('caffè');
      buffer.append(bytes.sublist(0, 5));
      buffer.append(bytes.sublist(5));

      expect(buffer.text, 'caffè');
    });

    test('the ring drops the oldest bytes and says so', () {
      final buffer = TerminalBuffer(maxBytes: 8);
      buffer.append(utf8.encode('aaaaaaaaaa'));
      buffer.append(utf8.encode('bbbbbbbbbb'));

      expect(buffer.length, 8);
      expect(buffer.isTruncated, isTrue);
      expect(buffer.text, 'bbbbbbbb');
    });

    test('clear forgets everything, truncation included', () {
      final buffer = TerminalBuffer(maxBytes: 4);
      buffer.append(utf8.encode('123456'));
      buffer.clear();

      expect(buffer.text, '');
      expect(buffer.isTruncated, isFalse);
    });

    test('an empty chunk changes nothing', () {
      final buffer = TerminalBuffer();
      buffer.append(const []);
      expect(buffer.length, 0);
      expect(buffer.text, '');
    });

    test('malformed bytes are shown as replacement, not thrown', () {
      final buffer = TerminalBuffer();
      buffer.append(const [0x61, 0xff, 0x62]);

      expect(buffer.text, contains('a'));
      expect(buffer.text, contains('b'));
    });
  });
}
