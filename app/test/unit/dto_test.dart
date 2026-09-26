import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/models/chat_entry.dart';
import 'package:piui/core/models/session.dart';

void main() {
  group('ServerIdentity', () {
    test('reads the limits, the features and the fingerprint', () {
      final identity = ServerIdentity.fromJson({
        'version': '0.1.0',
        'piVersion': '0.87.1',
        'protocol': 1,
        'features': ['sessions', 'replay', 7],
        'limits': {'maxSessions': 8, 'pairingTtlSec': 600},
        'tls': {'fingerprintSha256': '4F2A'},
      });
      expect(identity.isCompatible, isTrue);
      expect(identity.has('replay'), isTrue);
      expect(identity.has('terminal'), isFalse);
      expect(identity.maxSessions, 8);
      expect(identity.pairingTtlSec, 600);
      expect(identity.fingerprint, '4f2a');
      expect(identity.withFingerprint('aa').fingerprint, 'aa');
    });

    test('a missing version degrades instead of throwing', () {
      final identity = ServerIdentity.fromJson(const {});
      expect(identity.version, 'unknown');
      expect(identity.protocol, 1);
      expect(identity.features, isEmpty);
    });

    test('round-trips through the profile JSON', () {
      final identity = ServerIdentity.fromJson({
        'version': '1.2.3',
        'piVersion': '0.87.1',
        'protocol': 1,
        'features': ['sessions'],
        'limits': {'maxSessions': 4},
        'tls': {'fingerprintSha256': 'ab'},
      });
      final restored = ServerIdentity.fromJson(identity.toJson());
      expect(restored.version, '1.2.3');
      expect(restored.maxSessions, 4);
      expect(restored.fingerprint, 'ab');
    });

    test('protocol 2 is not this client', () {
      expect(ServerIdentity.fromJson({'protocol': 2}).isCompatible, isFalse);
    });
  });

  group('PairResult', () {
    test('reads the token, the scope and the server', () {
      final result = PairResult.fromJson({
        'deviceId': 'd_4f2a',
        'token': 'd_4f2a.secret',
        'scope': 'operator',
        'expiresAt': '2026-10-26T12:00:00.000Z',
        'server': {'version': '0.1.0', 'piVersion': '0.87.1', 'protocol': 1},
      });
      expect(result.deviceId, 'd_4f2a');
      expect(result.token, 'd_4f2a.secret');
      expect(result.scope, DeviceScope.operator);
      expect(result.expiresAt, isNotNull);
      expect(result.server.piVersion, '0.87.1');
    });

    test('an unknown scope is the least privilege', () {
      expect(DeviceScope.parse('root'), DeviceScope.viewer);
      expect(DeviceScope.viewer.covers(DeviceScope.operator), isFalse);
      expect(DeviceScope.admin.covers(DeviceScope.operator), isTrue);
      expect(DeviceScope.operator.covers(DeviceScope.operator), isTrue);
    });
  });

  group('DeviceInfo', () {
    test('reads deviceId, not id', () {
      final device = DeviceInfo.fromJson({
        'deviceId': 'd_9',
        'name': "Alessandro's Pixel",
        'scope': 'admin',
        'current': true,
        'createdAt': '2026-09-26T12:00:00.000Z',
      });
      expect(device.id, 'd_9');
      expect(device.displayName, "Alessandro's Pixel");
      expect(device.scope, DeviceScope.admin);
      expect(device.current, isTrue);
    });

    test('falls back to the id as a name', () {
      expect(DeviceInfo.fromJson({'deviceId': 'd_9'}).displayName, 'd_9');
    });
  });

  group('sessionFromJson', () {
    test('maps the server projection onto the UI model', () {
      final session = sessionFromJson({
        'id': 's_1',
        'cwd': '/home/user/Projects/pi-ui',
        'name': 'pi-ui',
        'status': 'streaming',
        'pid': 42,
        'piSessionId': 'abc',
        'modelProvider': 'llama.cpp',
        'modelId': 'qwen',
        'thinkingLevel': 'low',
        'createdAt': '2026-09-26T12:00:00.000Z',
        'lastEventAt': '2026-09-26T12:01:00.000Z',
        'exitCode': 3,
        'unknownField': 'kept on the wire, ignored here',
      });
      expect(session.id, 's_1');
      expect(session.status, SessionStatus.streaming);
      expect(session.pid, 42);
      expect(session.provider, 'llama.cpp');
      expect(session.modelId, 'qwen');
      expect(session.thinkingLevel, 'low');
      expect(session.exitCode, 3);
      expect(session.displayName, 'pi-ui');
      expect(session.lastEventAt, isNotNull);
    });

    test('an unknown status reads as exited, never as live', () {
      expect(sessionStatusFrom('garbage'), SessionStatus.exited);
      expect(sessionStatusFrom(null), SessionStatus.exited);
      expect(sessionStatusFrom('ready'), SessionStatus.ready);
    });

    test('a missing cwd does not throw', () {
      final session = sessionFromJson(const {});
      expect(session.cwd, '');
      expect(session.status, SessionStatus.exited);
      expect(session.messageCount, 0);
    });
  });

  group('dialogMethodFrom', () {
    test('maps the four answerable methods', () {
      expect(dialogMethodFrom('confirm'), DialogMethod.confirm);
      expect(dialogMethodFrom('input'), DialogMethod.input);
      expect(dialogMethodFrom('editor'), DialogMethod.editor);
      expect(dialogMethodFrom('select'), DialogMethod.select);
      expect(dialogMethodFrom('nonsense'), DialogMethod.select);
    });
  });
}
