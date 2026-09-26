import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/client.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/profile.dart';

import '../support/fake_server.dart';

void main() {
  late FakeServer server;

  setUp(() async {
    server = await FakeServer.start();
  });

  tearDown(() => server.stop());

  test('health needs no credential', () async {
    final client = PiUiClient(
      profile: ServerProfile(baseUrl: server.baseUrl, deviceName: 'test'),
    );
    expect(await client.health(), isTrue);
  });

  test('pairing sends the code and reads the token', () async {
    final result = await PiUiClient.pair(
      baseUrl: server.baseUrl,
      deviceName: 'test device',
      code: '4K9M27',
      platform: 'linux',
    );
    expect(result.token, 'd_test.secret');
    expect(result.deviceId, 'd_test');
    expect(result.scope, DeviceScope.operator);
    expect(result.server.maxSessions, 4);
    expect(server.requests.single, {
      'deviceName': 'test device',
      'code': '4K9M27',
      'platform': 'linux',
    });
  });

  test('a refused pairing is one coded unauthorized', () async {
    server.rejectPairing = true;
    await expectLater(
      PiUiClient.pair(
        baseUrl: server.baseUrl,
        deviceName: 'test device',
        code: 'wrong',
      ),
      throwsA(
        isA<PiuiException>()
            .having((error) => error.code, 'code', ErrorCodes.unauthorized)
            .having((error) => error.isUnauthorized, 'isUnauthorized', isTrue),
      ),
    );
  });

  test(
    'an unreachable server is a coded failure, never a raw exception',
    () async {
      final client = PiUiClient(
        profile: const ServerProfile(
          baseUrl: 'http://127.0.0.1:1',
          deviceName: 'test',
        ),
      );
      await expectLater(
        client.sessions(),
        throwsA(
          isA<PiuiException>().having(
            (error) => error.code,
            'code',
            ErrorCodes.unreachable,
          ),
        ),
      );
    },
  );

  test('sessions are read from the list envelope', () async {
    final client = PiUiClient(
      profile: ServerProfile(baseUrl: server.baseUrl, deviceName: 'test'),
    );
    expect(await client.sessions(), isEmpty);
  });

  test('the workspaces and a listing come back typed', () async {
    final client = PiUiClient(
      profile: ServerProfile(baseUrl: server.baseUrl, deviceName: 'test'),
    );

    final roots = await client.workspaces();
    expect(roots.single.id, 'app');
    expect(roots.single.path, '/srv/app');
    expect(roots.single.label, 'app');

    final entries = await client.listDirectory('/srv/app');
    expect(entries.single.name, 'README.md');
    expect(entries.single.isDir, isFalse);
    expect(entries.single.sha256, 'abc');

    final content = await client.readFile('/srv/app/README.md');
    expect(content.text, 'hello');
    expect(content.isBinary, isFalse);
    expect(content.entry.path, '/srv/app/README.md');
  });

  test('refresh hands the rotated token to the callback', () async {
    PairResult? rotated;
    final client = PiUiClient(
      profile: ServerProfile(
        baseUrl: server.baseUrl,
        deviceName: 'test',
        token: 'd_test.secret',
      ),
      onTokenRotated: (result) => rotated = result,
    );
    final result = await client.refresh();
    expect(result.token, 'd_test.rotated');
    expect(rotated?.token, 'd_test.rotated');
  });
}
