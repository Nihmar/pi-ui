import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/frames.dart';
import 'package:piui/core/api/session_store.dart';
import 'package:piui/core/models/session.dart';

WsEvent event(
  String type,
  Map<String, dynamic> payload, {
  String sessionId = 's_1',
  int seq = 1,
}) => parseFrame(
  jsonEncode({
    'type': type,
    'sessionId': sessionId,
    'seq': seq,
    'ts': '2026-09-26T12:00:00.000Z',
    'payload': payload,
  }),
) as WsEvent;

SessionModel seed() => SessionModel(
  id: 's_1',
  cwd: '/home/user/project',
  status: SessionStatus.ready,
  createdAt: DateTime.utc(2026, 9, 26),
);

void main() {
  group('lifecycle frames', () {
    test('server.status replaces the projection of that session', () {
      final folded = applySessionFrame(
        [seed()],
        event('server.status', {
          'id': 's_1',
          'cwd': '/home/user/project',
          'status': 'streaming',
          'modelProvider': 'llama.cpp',
          'modelId': 'qwen',
          'thinkingLevel': 'medium',
        }),
      );
      expect(folded.single.status, SessionStatus.streaming);
      expect(folded.single.modelId, 'qwen');
      expect(folded.single.thinkingLevel, 'medium');
    });

    test('server.spawned appends a session the list never saw', () {
      final folded = applySessionFrame(
        [seed()],
        event('server.spawned', {
          'id': 's_2',
          'cwd': '/home/user/other',
          'status': 'spawning',
          'createdAt': '2026-09-26T12:00:01.000Z',
        }),
      );
      expect(folded.map((session) => session.id), ['s_1', 's_2']);
    });

    test('exited and crashed set the status and the exit code', () {
      final exited = applySessionFrame([
        seed(),
      ], event('server.exited', {'exitCode': 0}));
      expect(exited.single.status, SessionStatus.exited);
      expect(exited.single.exitCode, 0);

      final crashed = applySessionFrame([
        seed(),
      ], event('server.crashed', {'exitCode': 137}));
      expect(crashed.single.status, SessionStatus.crashed);
      expect(crashed.single.exitCode, 137);
    });

    test('a patch for an unknown session leaves the list alone', () {
      final sessions = [seed()];
      expect(
        identical(
          applySessionFrame(
            sessions,
            event('pi.agent_settled', const {}, sessionId: 's_9'),
          ),
          sessions,
        ),
        isTrue,
      );
    });

    test('a lifecycle event with no id is not a session', () {
      final sessions = [seed()];
      expect(
        identical(
          applySessionFrame(sessions, event('server.status', const {})),
          sessions,
        ),
        isTrue,
      );
    });

    test('an unchanged patch returns the same list, so nothing is emitted', () {
      final sessions = [seed()];
      expect(
        identical(
          applySessionFrame(
            sessions,
            event('pi.queue_update', const {'steering': <Object>[]}),
          ),
          sessions,
        ),
        isTrue,
      );
    });
  });

  group('live details', () {
    test('the name comes from session_info_changed', () {
      final folded = applySessionFrame([
        seed(),
      ], event('pi.session_info_changed', {'name': 'pi-ui'}));
      expect(folded.single.name, 'pi-ui');
      expect(folded.single.displayName, 'pi-ui');
    });

    test('the thinking level comes from its own event', () {
      final folded = applySessionFrame([
        seed(),
      ], event('pi.thinking_level_changed', {'level': 'high'}));
      expect(folded.single.thinkingLevel, 'high');
    });

    test('the queue depth is counted from pi.queue_update', () {
      final folded = applySessionFrame(
        [seed()],
        event('pi.queue_update', {
          'steering': ['a'],
          'followUp': ['b', 'c'],
        }),
      );
      expect(folded.single.pendingMessages, 3);
    });

    test('a run in flight flips the status and a settle flips it back', () {
      var sessions = [seed()];
      sessions = applySessionFrame(
        sessions,
        event('pi.message_update', const {}),
      );
      expect(sessions.single.status, SessionStatus.streaming);

      sessions = applySessionFrame(
        sessions,
        event('pi.agent_settled', const {}),
      );
      expect(sessions.single.status, SessionStatus.ready);
    });

    test('message entries count towards messageCount', () {
      var sessions = [seed()];
      for (final role in const ['user', 'assistant', 'toolResult']) {
        sessions = applySessionFrame(
          sessions,
          event('pi.entry_appended', {
            'entry': {
              'id': 'e',
              'type': 'message',
              'message': {'role': role},
            },
          }),
        );
      }
      expect(sessions.single.messageCount, 3);
      // A non-message entry (a label, a thinking-level change) does not count.
      sessions = applySessionFrame(
        sessions,
        event('pi.entry_appended', {
          'entry': {'id': 'e', 'type': 'label'},
        }),
      );
      expect(sessions.single.messageCount, 3);
    });
  });
}
