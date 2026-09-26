import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/profile.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  group('normalizeServerUrl', () {
    test('accepts a bare host, a host:port and a full origin', () {
      expect(normalizeServerUrl('pi-ui.local'), 'http://pi-ui.local');
      expect(normalizeServerUrl('pi-ui.local:8787'), 'http://pi-ui.local:8787');
      expect(
        normalizeServerUrl('https://pi-ui.example/pi-ui/'),
        'https://pi-ui.example/pi-ui',
      );
    });

    test('reads ws and wss as http and https', () {
      expect(
        normalizeServerUrl('ws://pi-ui.local:8787'),
        'http://pi-ui.local:8787',
      );
      expect(
        normalizeServerUrl('wss://pi-ui.example'),
        'https://pi-ui.example',
      );
    });

    test('rejects what is not an origin', () {
      expect(() => normalizeServerUrl(''), throwsA(isA<PiuiException>()));
      expect(
        () => normalizeServerUrl('ftp://pi-ui.local'),
        throwsA(
          isA<PiuiException>().having(
            (error) => error.code,
            'code',
            ErrorCodes.badRequest,
          ),
        ),
      );
      expect(
        () => normalizeServerUrl('http://'),
        throwsA(isA<PiuiException>()),
      );
    });
  });

  group('ServerProfile', () {
    test('builds the REST base and the WebSocket endpoint', () {
      const profile = ServerProfile(
        baseUrl: 'https://pi-ui.example/pi-ui',
        deviceName: 'phone',
      );
      expect(profile.apiBase.toString(), 'https://pi-ui.example/pi-ui/api/v1');
      expect(profile.wsUri.toString(), 'wss://pi-ui.example/pi-ui/ws/v1');
      expect(profile.host, 'pi-ui.example');
      expect(profile.isPaired, isFalse);
    });

    test('plain http gets a plain ws', () {
      const profile = ServerProfile(
        baseUrl: 'http://10.0.0.5:8787',
        deviceName: 'x',
      );
      expect(profile.wsUri.toString(), 'ws://10.0.0.5:8787/ws/v1');
    });

    test('toJson never carries the token', () {
      const profile = ServerProfile(
        baseUrl: 'http://pi-ui.local:8787',
        deviceName: 'phone',
        token: 'd_1.secret',
        deviceId: 'd_1',
      );
      expect(profile.toJson().containsKey('token'), isFalse);
      expect(profile.toJson()['deviceId'], 'd_1');
    });
  });

  group('PrefsProfileStore', () {
    late MemoryTokenStore tokens;
    late PrefsProfileStore store;

    setUp(() {
      SharedPreferences.setMockInitialValues({});
      tokens = MemoryTokenStore();
      store = PrefsProfileStore(tokens: tokens);
    });

    test('nothing stored means unpaired', () async {
      expect(await store.read(), isNull);
    });

    test('round-trips a profile and keeps the token in the keystore', () async {
      const profile = ServerProfile(
        baseUrl: 'http://pi-ui.local:8787',
        deviceName: 'phone',
        token: 'd_1.secret',
        deviceId: 'd_1',
      );
      await store.write(profile);

      final restored = await store.read();
      expect(restored?.baseUrl, 'http://pi-ui.local:8787');
      expect(restored?.token, 'd_1.secret');
      expect(await tokens.read(), 'd_1.secret');
    });

    test('writeToken replaces the secret and nothing else', () async {
      await store.write(
        const ServerProfile(
          baseUrl: 'http://pi-ui.local:8787',
          deviceName: 'phone',
          token: 'd_1.old',
          deviceId: 'd_1',
        ),
      );
      final rotated = await store.writeToken('d_1.new');
      expect(rotated?.token, 'd_1.new');
      expect(rotated?.deviceId, 'd_1');
      expect(await tokens.read(), 'd_1.new');
    });

    test('clear forgets both halves', () async {
      await store.write(
        const ServerProfile(
          baseUrl: 'http://pi-ui.local:8787',
          deviceName: 'phone',
          token: 'd_1.secret',
        ),
      );
      await store.clear();
      expect(await store.read(), isNull);
      expect(await tokens.read(), isNull);
    });
  });

  group('MemoryProfileStore', () {
    test('acts like the real one, without a platform channel', () async {
      final store = MemoryProfileStore();
      expect(await store.read(), isNull);
      await store.write(
        const ServerProfile(baseUrl: 'http://a', deviceName: 'n', token: 't'),
      );
      expect((await store.read())?.token, 't');
      expect(await store.writeToken('t2'), isNotNull);
      expect((await store.read())?.token, 't2');
      await store.clear();
      expect(await store.read(), isNull);
    });
  });
}
