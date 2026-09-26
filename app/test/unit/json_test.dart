import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/json.dart';

void main() {
  group('json helpers', () {
    test('narrow without casting', () {
      expect(asMap({'a': 1})?['a'], 1);
      expect(asMap([1, 2]), isNull);
      expect(asMap(null), isNull);
      expect(asList([1, 2]).length, 2);
      expect(asList('nope'), isEmpty);
      expect(
        asMapList([
          {'a': 1},
          2,
          'x',
        ]).single['a'],
        1,
      );
    });

    test('strings are only strings', () {
      expect(str('a'), 'a');
      expect(str(2), '');
      expect(str(2, fallback: '?'), '?');
      expect(optStr(''), isNull);
      expect(optStr('a'), 'a');
      expect(optStr(1), isNull);
    });

    test('numbers and booleans are read leniently', () {
      expect(intOf(2.9), 2);
      expect(intOf('3'), 0);
      expect(optInt('3'), isNull);
      expect(doubleOf(1), 1.0);
      expect(doubleOf('1', fallback: -1), -1.0);
      expect(boolOf(true), isTrue);
      expect(boolOf('true'), isFalse);
    });

    test('timestamps parse RFC3339 and milliseconds', () {
      expect(timeOf('2026-09-26T12:00:00.000Z')?.isUtc, isFalse);
      expect(timeOf(1)?.millisecondsSinceEpoch, 1);
      expect(timeOf('nonsense'), isNull);
      expect(timeOf(null), isNull);
    });
  });
}
