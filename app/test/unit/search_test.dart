import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/search.dart';

void main() {
  group('SearchHit', () {
    test('reads a file hit', () {
      final hits = SearchHit.listFrom({
        'hits': [
          {
            'kind': 'file',
            'path': '/srv/app/lib/main.dart',
            'rel': 'lib/main.dart',
            'rootId': 'app',
            'line': 12,
            'column': 3,
            'text': 'void main() {',
          },
        ],
      });

      final hit = hits.single;
      expect(hit.isMessage, isFalse);
      expect(hit.title, 'lib/main.dart');
      expect(hit.location, '/srv/app/lib/main.dart:12');
      expect(hit.text, 'void main() {');
    });

    test('reads a message hit', () {
      final hit = SearchHit.fromJson({
        'kind': 'message',
        'path': '/home/u/.pi/agent/sessions/x.jsonl',
        'text': '…the RpcBridge framing…',
        'sessionId': '01a0dccc',
        'role': 'user',
        'at': '2026-09-26T12:00:00Z',
      });

      expect(hit.isMessage, isTrue);
      expect(hit.title, '01a0dccc');
      expect(hit.location, startsWith('user · '));
    });

    test('a path with no rel falls back to its name', () {
      final hit = SearchHit.fromJson({'path': '/srv/a/b/notes.md'});
      expect(hit.title, 'notes.md');
      expect(hit.kind, 'file', reason: 'an unknown kind reads as a file');
    });

    test('an empty answer is empty, not a crash', () {
      expect(SearchHit.listFrom(null), isEmpty);
      expect(SearchHit.listFrom(const {'hits': 'nope'}), isEmpty);
      expect(SearchHit.fromJson(null).path, '');
      expect(SearchHit.fromJson(null).title, '');
    });
  });
}
