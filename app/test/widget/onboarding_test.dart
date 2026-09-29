import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/app.dart';
import 'package:piui/core/models/session.dart';
import 'package:piui/features/onboarding/pair_screen.dart';

import '../support/test_app.dart';

/// What the fake pairing seam recorded.
class PairingCall {
  const PairingCall({
    required this.baseUrl,
    required this.deviceName,
    this.code,
    this.secret,
    this.password,
    this.fingerprint,
  });

  final String baseUrl;
  final String deviceName;
  final String? code;
  final String? secret;
  final String? password;
  final String? fingerprint;
}

/// A [Pairer] the test drives: it records the call and answers with a token (or
/// with the coded failure a wrong code produces). [serverFingerprint] is what the
/// paired server reports under `server.tls.fingerprintSha256`.
Pairer fakePairer(
  List<PairingCall> calls, {
  bool reject = false,
  String? serverFingerprint,
}) {
  return ({
    required String baseUrl,
    required String deviceName,
    String? code,
    String? secret,
    String? password,
    String? fingerprint,
  }) async {
    calls.add(
      PairingCall(
        baseUrl: baseUrl,
        deviceName: deviceName,
        code: code,
        secret: secret,
        password: password,
        fingerprint: fingerprint,
      ),
    );
    if (reject) {
      throw const PiuiException(
        ErrorCodes.unauthorized,
        'wrong or expired code',
      );
    }
    return PairResult(
      deviceId: 'd_test',
      token: 'd_test.secret',
      scope: DeviceScope.operator,
      server: ServerIdentity(
        version: '0.1.0',
        piVersion: '0.87.1',
        protocol: 1,
        features: const ['sessions'],
        fingerprint: serverFingerprint,
      ),
    );
  };
}

Future<void> pumpOnboarding(
  WidgetTester tester, {
  required MemoryProfileStore store,
  required List<PairingCall> calls,
  bool reject = false,
  bool reachable = true,
  String? serverFingerprint,
}) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        profileStoreProvider.overrideWithValue(store),
        // The network is not what these tests are about: the seams are the
        // providers the screens call, and the real calls are covered by
        // test/unit/client_test.dart against a real socket.
        socketProvider.overrideWithValue(null),
        sessionsProvider.overrideWith(
          (ref) => Stream.value(const <SessionModel>[]),
        ),
        healthProbeProvider.overrideWithValue((_) async => reachable),
        pairerProvider.overrideWithValue(
          fakePairer(
            calls,
            reject: reject,
            serverFingerprint: serverFingerprint,
          ),
        ),
      ],
      child: const PiuiApp(),
    ),
  );
  await tester.pumpAndSettle();
}

/// Pumps just the pairing screen, without the router.
///
/// The full app redirects off `/onboarding` the moment the profile is paired, so
/// the fingerprint dialog — which runs *after* the pair call — needs the screen
/// on its own to be observed.
Future<void> pumpPairScreen(
  WidgetTester tester, {
  required MemoryProfileStore store,
  required List<PairingCall> calls,
  String? serverFingerprint,
}) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        profileStoreProvider.overrideWithValue(store),
        healthProbeProvider.overrideWithValue((_) async => true),
        pairerProvider.overrideWithValue(
          fakePairer(calls, serverFingerprint: serverFingerprint),
        ),
      ],
      child: testApp(home: const PairScreen()),
    ),
  );
  await tester.pumpAndSettle();
}

const _address = Key('pair-address');
const _code = Key('pair-code');
const _password = Key('pair-password');

String _addressText(WidgetTester tester) =>
    tester.widget<TextField>(find.byKey(_address)).controller!.text;

String _codeText(WidgetTester tester) =>
    tester.widget<TextField>(find.byKey(_code)).controller!.text;

void main() {
  late MemoryProfileStore store;
  late List<PairingCall> calls;

  setUp(() {
    store = MemoryProfileStore();
    calls = [];
  });

  testWidgets('an unpaired client starts at the single pairing screen', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    expect(find.text('Pair with the server'), findsOneWidget);
    expect(find.byKey(_address), findsOneWidget);
    expect(find.byKey(_code), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('a refused URL stays on the address field with the reason', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byKey(_address), 'ftp://pi-ui.local');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Only http and https'), findsOneWidget);
    expect(calls, isEmpty);
  });

  testWidgets('the probe reports a reachable server', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.text('reachable'), findsOneWidget);
  });

  testWidgets('the probe reports an unreachable server', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls, reachable: false);

    await tester.enterText(find.byKey(_address), 'pi-ui.local');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.text('reachable'), findsNothing);
    expect(find.textContaining('not with "ok"'), findsOneWidget);
  });

  testWidgets('pasting a pairing link fills the address and the code', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(
      find.byKey(_address),
      'piui://pair?v=1&url=http%3A%2F%2Fpi-ui.local%3A8787&code=4k9m27',
    );
    await tester.pumpAndSettle();

    expect(_addressText(tester), 'http://pi-ui.local:8787');
    expect(_codeText(tester), '4K9M27');

    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(calls.single.baseUrl, 'http://pi-ui.local:8787');
    expect(calls.single.code, '4K9M27');
    expect(store.current?.token, 'd_test.secret');
  });

  testWidgets('a link fingerprint the server repeats is trusted silently', (
    tester,
  ) async {
    await pumpOnboarding(
      tester,
      store: store,
      calls: calls,
      serverFingerprint: 'abcd',
    );

    await tester.enterText(
      find.byKey(_address),
      'piui://pair?v=1&url=http%3A%2F%2Fpi-ui.local%3A8787&code=4k9m27&fp=abcd',
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.text('Trust this certificate?'), findsNothing);
    expect(calls.single.fingerprint, 'abcd');
    expect(store.current?.token, 'd_test.secret');
  });

  testWidgets('a link fingerprint the server does not repeat is a hard error', (
    tester,
  ) async {
    // The server reports no fingerprint at all, so the link's pin cannot hold.
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(
      find.byKey(_address),
      'piui://pair?v=1&url=http%3A%2F%2Fpi-ui.local%3A8787&code=4k9m27&fp=abcd',
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.textContaining('does not match the link'), findsOneWidget);
    expect(store.current, isNull);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('a fingerprint no link carried is confirmed by a dialog', (
    tester,
  ) async {
    await pumpPairScreen(
      tester,
      store: store,
      calls: calls,
      serverFingerprint: 'abcd',
    );

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    await tester.enterText(find.byKey(_code), '4K9M27');
    await tester.tap(find.text('Pair'));
    // Not pumpAndSettle: the Pair button's progress spinner keeps animating while
    // the modal waits for an answer, so the tree never settles.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('Trust this certificate?'), findsOneWidget);
    await tester.tap(find.text('Trust and continue'));
    await tester.pumpAndSettle();

    expect(store.current?.token, 'd_test.secret');
  });

  testWidgets('refusing the certificate forgets the just-written token', (
    tester,
  ) async {
    await pumpPairScreen(
      tester,
      store: store,
      calls: calls,
      serverFingerprint: 'abcd',
    );

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    await tester.enterText(find.byKey(_code), '4K9M27');
    await tester.tap(find.text('Pair'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(store.current, isNull);
  });

  testWidgets('a wrong code keeps the user on the pairing screen', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls, reject: true);

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    await tester.enterText(find.byKey(_code), '4K9M27');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.textContaining('wrong or expired code'), findsOneWidget);
    expect(find.textContaining('consumed, expired or wrong'), findsOneWidget);
    expect(store.current, isNull);
    expect(calls.single.code, '4K9M27');
  });

  testWidgets('pairing stores the token and lands on the session list', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    // Lower case and a separator on purpose: the screen normalizes before sending.
    await tester.enterText(find.byKey(_code), '4k9-m27');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.text('Sessions'), findsWidgets);
    expect(store.current?.token, 'd_test.secret');
    expect(store.current?.baseUrl, 'http://pi-ui.local:8787');
    expect(store.current?.scope, DeviceScope.operator);
    expect(calls.single.deviceName, isNotEmpty);
    expect(calls.single.code, '4K9M27');
  });

  testWidgets('the admin password branch pairs a device with a password', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byKey(_address), 'pi-ui.local:8787');
    await tester.tap(find.text('Pair with the admin password'));
    await tester.pumpAndSettle();

    // The code field is replaced by the password one, and the password is required.
    expect(find.byKey(_code), findsNothing);
    expect(find.byKey(_password), findsOneWidget);
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();
    expect(find.text('Type the admin password.'), findsOneWidget);
    expect(calls, isEmpty);

    await tester.enterText(find.byKey(_password), 'correct horse');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(calls.single.password, 'correct horse');
    expect(calls.single.code, isNull);
    expect(store.current?.token, 'd_test.secret');
  });

  testWidgets('the Scan QR button is shown on Android', (tester) async {
    // Reset inside the body: the binding asserts the foundation debug variables
    // are unset before a tearDown would run.
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    await pumpOnboarding(tester, store: store, calls: calls);

    expect(find.text('Scan QR'), findsOneWidget);
    debugDefaultTargetPlatformOverride = null;
  });

  testWidgets('the Scan QR button is hidden off Android', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.linux;
    await pumpOnboarding(tester, store: store, calls: calls);

    expect(find.text('Scan QR'), findsNothing);
    debugDefaultTargetPlatformOverride = null;
  });
}
