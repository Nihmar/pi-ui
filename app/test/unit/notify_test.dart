import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/frames.dart';
import 'package:piui/core/notify.dart';

WsEvent frame(
  String type, {
  String sessionId = 's_1',
  Map<String, dynamic> payload = const {},
}) => parseFrame(
  jsonEncode({
    'type': type,
    'sessionId': sessionId,
    'seq': 1,
    'ts': '2026-09-26T12:00:00.000Z',
    'payload': payload,
  }),
) as WsEvent;

void main() {
  late StreamController<WsFrame> frames;
  late RecordingNotifier notifier;
  late SessionWatcher watcher;
  var foreground = false;

  setUp(() {
    frames = StreamController<WsFrame>.broadcast();
    notifier = RecordingNotifier();
    foreground = false;
    watcher = SessionWatcher(
      frames: frames.stream,
      notifier: notifier,
      isForeground: () => foreground,
      titleOf: (sessionId) => sessionId == 's_1' ? 'pi-ui' : 'other',
    )..start();
  });

  tearDown(() async {
    await watcher.dispose();
    await frames.close();
  });

  test('a settled run notifies while the app is in the background', () async {
    frames.add(frame('pi.agent_settled'));
    await pumpEventQueue();

    expect(notifier.shown, hasLength(1));
    expect(notifier.shown.single.title, 'pi-ui');
    expect(notifier.shown.single.body, 'The run finished.');
  });

  test('nothing is shown while the app is on screen', () async {
    foreground = true;
    frames.add(frame('pi.agent_settled'));
    await pumpEventQueue();

    expect(notifier.shown, isEmpty);
  });

  test('a crashed child notifies with its exit code', () async {
    frames.add(frame('server.crashed', payload: {'exitCode': 137}));
    await pumpEventQueue();

    expect(notifier.shown.single.body, contains('137'));
  });

  test('the ordinary stream is not worth an interruption', () async {
    frames
      ..add(frame('pi.entry_appended'))
      ..add(frame('pi.message_update'))
      ..add(frame('server.status'))
      ..add(frame('pi.queue_update'));
    await pumpEventQueue();

    expect(notifier.shown, isEmpty);
  });

  test('a server-wide event has no session to notify about', () async {
    frames.add(
      parseFrame(
        jsonEncode({
          'type': 'server.heartbeat',
          'seq': 9,
          'payload': {'uptimeSec': 1},
        }),
      ),
    );
    await pumpEventQueue();

    expect(notifier.shown, isEmpty);
  });

  test('start is idempotent, so a rebuild does not double-notify', () async {
    watcher.start();
    frames.add(frame('pi.agent_settled'));
    await pumpEventQueue();

    expect(notifier.shown, hasLength(1));
  });

  test('a stable id per session replaces the previous notification', () {
    expect(LocalNotifier.notificationId('s_1'), isNonNegative);
    expect(
      LocalNotifier.notificationId('s_1'),
      LocalNotifier.notificationId('s_1'),
    );
    expect(
      LocalNotifier.notificationId('s_1'),
      isNot(LocalNotifier.notificationId('s_2')),
    );
  });

  test('the silent notifier swallows everything', () async {
    const silent = SilentNotifier();
    await silent.initialize();
    await silent.sessionSettled(sessionId: 's_1', title: 'x');
    await silent.sessionFailed(sessionId: 's_1', title: 'x', body: 'y');
  });
}
