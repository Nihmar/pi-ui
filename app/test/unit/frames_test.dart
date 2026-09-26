import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/frames.dart';
import 'package:piui/core/models/chat_entry.dart';

void main() {
  group('parseFrame', () {
    test('welcome carries the identity and the heartbeat', () {
      final frame = parseFrame(
        jsonEncode({
          'type': 'welcome',
          'v': 1,
          'server': {'version': '0.1.0', 'piVersion': '0.87.1', 'protocol': 1},
          'heartbeatSec': 30,
        }),
      );
      expect(frame, isA<WsWelcome>());
      final welcome = frame as WsWelcome;
      expect(welcome.version, 1);
      expect(welcome.identity.piVersion, '0.87.1');
      expect(welcome.heartbeatSec, 30);
    });

    test('an event keeps its payload, cursor and session', () {
      final frame = parseFrame(
        jsonEncode({
          'type': 'pi.entry_appended',
          'sessionId': 's_1',
          'seq': 42,
          'entryId': 'e-7',
          'ts': '2026-09-26T12:00:00.000Z',
          'payload': {
            'entry': {'id': 'e-7'},
          },
        }),
      ) as WsEvent;
      expect(frame.type, 'pi.entry_appended');
      expect(frame.sessionId, 's_1');
      expect(frame.seq, 42);
      expect(frame.entryId, 'e-7');
      expect(frame.isMeta, isFalse);
      expect(frame.at, isNotNull);
    });

    test('the meta frames are marked, so they never move a cursor', () {
      for (final type in [
        'server.heartbeat',
        'server.replay.begin',
        'server.replay.end',
      ]) {
        final frame =
            parseFrame(jsonEncode({'type': type, 'seq': 9})) as WsEvent;
        expect(frame.isMeta, isTrue, reason: type);
      }
    });

    test('a request becomes a dialog with a deadline', () {
      final frame = parseFrame(
        jsonEncode({
          'type': 'request',
          'id': 'u1',
          'sessionId': 's_1',
          'method': 'confirm',
          'title': 'Bash',
          'message': 'run?',
          'timeoutMs': 60000,
          'ts': '2026-09-26T12:00:00.000Z',
        }),
      ) as WsRequest;
      expect(frame.method, DialogMethod.confirm);
      expect(frame.title, 'Bash');
      expect(frame.expiresAt.difference(frame.at).inSeconds, 60);
      final dialog = frame.toDialogRequest();
      expect(dialog.id, 'u1');
      expect(dialog.remainingSeconds(frame.at), 60);
    });

    test('a select keeps its options and a confirm does not need any', () {
      final select = parseFrame(
        jsonEncode({
          'type': 'request',
          'id': 'u2',
          'sessionId': 's_1',
          'method': 'select',
          'title': 'Pick',
          'options': ['a', 'b'],
        }),
      ) as WsRequest;
      expect(select.options, ['a', 'b']);
      expect(select.at, isNotNull);
    });

    test('a response reports success or the coded failure', () {
      final ok = parseFrame(
        jsonEncode({
          'type': 'response',
          'id': 'c1',
          'ok': true,
          'data': {'x': 1},
        }),
      ) as WsResponse;
      expect(ok.data?['x'], 1);
      expect(ok.failure, isNull);

      final failed = parseFrame(
        jsonEncode({
          'type': 'response',
          'id': 'c2',
          'ok': false,
          'error': {'code': 'rate_limited', 'message': 'slow down'},
        }),
      ) as WsResponse;
      expect(failed.failure?.code, ErrorCodes.rateLimited);
      expect(failed.failure?.message, 'slow down');
    });

    test('pong and an unknown frame stay readable', () {
      expect(parseFrame('{"type":"pong"}'), isA<WsPong>());
      final unknown = parseFrame('{"type":"weird","x":1}') as WsUnknown;
      expect(unknown.type, 'weird');
      expect(unknown.raw, contains('weird'));
    });

    test('text that is not a JSON object is a bad frame', () {
      expect(
        () => parseFrame('not json'),
        throwsA(
          isA<PiuiException>().having(
            (error) => error.code,
            'code',
            ErrorCodes.badFrame,
          ),
        ),
      );
      expect(() => parseFrame('[1,2]'), throwsA(isA<PiuiException>()));
    });
  });
}
