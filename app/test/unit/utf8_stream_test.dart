import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/features/terminal/utf8_stream.dart';

void main() {
  group('Utf8Stream', () {
    test('decodes what arrives in one piece', () {
      final stream = Utf8Stream();
      expect(stream.push(utf8.encode('hello')), 'hello');
      expect(stream.pendingBytes, 0);
    });

    test('holds the tail of a character split across chunks', () {
      final stream = Utf8Stream();
      // "è" is two bytes; a terminal read can end between them.
      final bytes = utf8.encode('caffè');

      expect(stream.push(bytes.sublist(0, 5)), 'caff');
      expect(stream.pendingBytes, 1, reason: 'the incomplete byte waits');

      expect(stream.push(bytes.sublist(5)), 'è');
      expect(stream.pendingBytes, 0);
    });

    test('a three-byte character split twice still decodes', () {
      final stream = Utf8Stream();
      final bytes = utf8.encode('日');

      expect(stream.push([bytes[0]]), '');
      expect(stream.push([bytes[1]]), '');
      expect(stream.push([bytes[2]]), '日');
    });

    test('malformed bytes are shown, not held forever', () {
      final stream = Utf8Stream();
      // A lone continuation byte can never become a character.
      expect(stream.push([0x61, 0x80, 0x62]), contains('a'));
      expect(stream.pendingBytes, 0);
    });

    test('an over-long tail is released as replacement characters', () {
      final stream = Utf8Stream();
      // Five bytes claiming to start a four-byte sequence: there is no completion coming.
      final text = stream.push([0xf0, 0x9f, 0x98, 0x80, 0x80, 0x80]);
      expect(text, isNotEmpty);
      expect(stream.pendingBytes, 0);
    });

    test('an empty chunk changes nothing', () {
      final stream = Utf8Stream();
      expect(stream.push(const []), '');
      expect(stream.pendingBytes, 0);
    });
  });
}
