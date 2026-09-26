import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/frames.dart';
import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/socket.dart';

import '../support/fake_channel.dart';

const _profile = ServerProfile(
  baseUrl: 'http://pi-ui.test:8787',
  deviceName: 'test',
  token: 'd_test.secret',
);

/// Waits for real timers (the socket's backoff and handshake deadlines) and
/// then lets the queued stream events drain.
Future<void> settle([int milliseconds = 10]) async {
  await Future<void>.delayed(Duration(milliseconds: milliseconds));
  await pumpEventQueue(times: 20);
}

/// A socket wired to a scripted fake channel: the test decides what the server
/// answers and reads what the client sent.
class SocketHarness {
  SocketHarness({this.answerHandshake = true, this.heartbeatSec = 60}) {
    socket = PiUiSocket(
      profile: _profile,
      channels: (uri, headers) {
        sentHeaders = headers;
        attempts++;
        channel = FakeChannel();
        return channel!;
      },
      handshakeTimeout: answerHandshake
          ? const Duration(seconds: 5)
          : const Duration(milliseconds: 40),
      commandTimeout: const Duration(seconds: 2),
      baseBackoff: const Duration(milliseconds: 5),
      maxBackoff: const Duration(milliseconds: 20),
      jitter: false,
      onProtocolError: errors.add,
    );
  }

  /// False makes the welcome never arrive, like a server that hangs up.
  final bool answerHandshake;
  final int heartbeatSec;
  late final PiUiSocket socket;

  /// The frames the client could not read.
  final errors = <PiuiException>[];

  FakeChannel? channel;
  Map<String, String>? sentHeaders;

  /// How many channels the socket asked for, so a test can see it retried.
  var attempts = 0;

  /// The frames the client put on the wire, decoded.
  List<Map<String, dynamic>> get sent => [
    for (final frame in channel?.sent ?? const [])
      if (frame is String) jsonDecode(frame) as Map<String, dynamic>,
  ];

  /// Starts the socket and answers the hello with a welcome.
  Future<void> connect() async {
    socket.start();
    await settle();
    if (answerHandshake) {
      welcome();
      await settle();
    } else {
      await settle(60);
    }
  }

  /// Sends the welcome frame, as a server does after a hello.
  void welcome() {
    channel?.serverSend(
      jsonEncode({
        'type': 'welcome',
        'v': 1,
        'server': {'version': '0.1.0', 'piVersion': '0.87.1', 'protocol': 1},
        'heartbeatSec': heartbeatSec,
      }),
    );
  }

  Future<void> dispose() => socket.dispose();
}

void main() {
  test('the handshake is a hello first, then a welcome', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);

    await harness.connect();

    expect(harness.socket.status, SocketStatus.online);
    expect(harness.sentHeaders?['Authorization'], 'Bearer d_test.secret');
    final hello = harness.sent.first;
    expect(hello['type'], 'hello');
    expect(hello['v'], 1);
    expect((hello['client'] as Map)['name'], 'pi-ui-client');
  });

  test('a refused handshake shows as reconnecting, not as an error', () async {
    final harness = SocketHarness(answerHandshake: false);
    addTearDown(harness.dispose);

    await harness.connect();

    expect(harness.socket.status.isWorking, isTrue);
    expect(harness.attempts, greaterThan(1), reason: 'it keeps retrying');
  });

  test(
    'the first subscribe replays, the next ones resume from the cursor',
    () async {
      final harness = SocketHarness();
      addTearDown(harness.dispose);
      await harness.connect();

      final frames = <WsFrame>[];
      harness.socket.frames.listen(frames.add);
      harness.socket.subscribe('s_1');
      await settle();

      final first = harness.sent.last;
      expect(first['type'], 'subscribe');
      expect(first['replay'], isTrue);
      expect(first.containsKey('since'), isFalse);

      harness.channel?.serverSend(
        jsonEncode({
          'type': 'pi.entry_appended',
          'sessionId': 's_1',
          'seq': 7,
          'entryId': 'e-7',
          'payload': {
            'entry': {'id': 'e-7'},
          },
        }),
      );
      await settle();
      expect(frames, hasLength(1));

      // A second subscribe (a reopened screen) asks for nothing: the history is
      // already in hand.
      harness.socket.subscribe('s_1');
      await settle();
      expect(harness.sent.last['replay'], isNull);
      expect(harness.sent.last['since'], {'entryId': 'e-7'});
    },
  );

  test('a dropped connection re-subscribes from the cursor it kept', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);
    await harness.connect();
    harness.socket.subscribe('s_1');
    await settle();
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'pi.entry_appended',
        'sessionId': 's_1',
        'seq': 9,
        'entryId': 'e-9',
        'payload': {
          'entry': {'id': 'e-9'},
        },
      }),
    );
    await settle();

    await harness.channel?.serverClose();
    await settle();
    expect(harness.socket.status, SocketStatus.reconnecting);

    // The retry lands on a fresh channel with a welcome.
    harness.welcome();
    await settle(30);

    expect(harness.socket.status, SocketStatus.online);
    final resubscribe = harness.sent.lastWhere(
      (frame) => frame['type'] == 'subscribe',
    );
    expect(resubscribe['since'], {'entryId': 'e-9'});
  });

  test('meta frames never move the cursor', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);
    await harness.connect();
    harness.socket.subscribe('s_1');
    await settle();
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'pi.entry_appended',
        'sessionId': 's_1',
        'seq': 4,
        'entryId': 'e-4',
        'payload': {
          'entry': {'id': 'e-4'},
        },
      }),
    );
    // The replay markers are stamped after the events they wrap: a cursor that
    // moved with them would cut off the replay.
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'server.replay.end',
        'sessionId': 's_1',
        'seq': 99,
        'payload': {'count': 1},
      }),
    );
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'server.heartbeat',
        'seq': 100,
        'payload': {'uptimeSec': 1},
      }),
    );
    await settle();

    harness.socket.subscribe('s_1');
    await settle();
    expect(harness.sent.last['since'], {'entryId': 'e-4'});
  });

  test(
    'a command is correlated with its response and its data returned',
    () async {
      final harness = SocketHarness();
      addTearDown(harness.dispose);
      await harness.connect();

      final pending = harness.socket.command(
        sessionId: 's_1',
        op: 'session.prompt',
        payload: {'message': 'hi'},
      );
      await settle();
      final command = harness.sent.last;
      expect(command['type'], 'command');
      expect(command['op'], 'session.prompt');
      expect((command['payload'] as Map)['message'], 'hi');

      harness.channel?.serverSend(
        jsonEncode({
          'type': 'response',
          'id': command['id'],
          'ok': true,
          'data': {'accepted': true},
        }),
      );
      expect((await pending)?['accepted'], true);
    },
  );

  test('a failed command throws the coded failure', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);
    await harness.connect();

    final pending = harness.socket.command(
      sessionId: 's_1',
      op: 'session.abort',
    );
    await settle();
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'response',
        'id': harness.sent.last['id'],
        'ok': false,
        'error': {'code': 'session_not_found', 'message': 'gone'},
      }),
    );
    await expectLater(
      pending,
      throwsA(
        isA<PiuiException>().having(
          (error) => error.code,
          'code',
          ErrorCodes.sessionNotFound,
        ),
      ),
    );
  });

  test('a command while offline fails at once, unless it may wait', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);

    await expectLater(
      harness.socket.command(sessionId: 's_1', op: 'session.prompt'),
      throwsA(
        isA<PiuiException>().having(
          (error) => error.code,
          'code',
          ErrorCodes.offline,
        ),
      ),
    );

    // With a budget, the send waits for the connection instead of failing.
    final waiting = harness.socket.command(
      sessionId: 's_1',
      op: 'session.prompt',
      payload: {'message': 'queued in flight'},
      waitForConnection: const Duration(seconds: 1),
    );
    await harness.connect();
    await settle();
    expect(
      harness.sent.any((frame) => frame['op'] == 'session.prompt'),
      isTrue,
    );
    harness.channel?.serverSend(
      jsonEncode({
        'type': 'response',
        'id': harness.sent.last['id'],
        'ok': true,
      }),
    );
    await waiting;
  });

  test('a lost connection fails the commands in flight', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);
    await harness.connect();

    final pending = harness.socket.command(
      sessionId: 's_1',
      op: 'session.abort',
    );
    await settle();
    await harness.channel?.serverClose();

    await expectLater(
      pending,
      throwsA(
        isA<PiuiException>().having(
          (error) => error.code,
          'code',
          ErrorCodes.offline,
        ),
      ),
    );
  });

  test(
    'an unreadable frame is reported without dropping the connection',
    () async {
      final harness = SocketHarness();
      addTearDown(harness.dispose);
      await harness.connect();

      harness.channel?.serverSend('not json');
      await settle();

      expect(harness.errors.single.code, ErrorCodes.badFrame);
      expect(harness.socket.status, SocketStatus.online);
    },
  );

  test('stop closes the socket and leaves it idle', () async {
    final harness = SocketHarness();
    addTearDown(harness.dispose);
    await harness.connect();

    await harness.socket.stop();

    expect(harness.socket.status, SocketStatus.idle);
    expect(harness.channel?.closed, isTrue);
  });
}
