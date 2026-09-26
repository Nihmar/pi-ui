import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/app.dart';
import 'package:piui/core/models/session.dart';

/// What the fake pairing seam recorded.
class PairingCall {
  const PairingCall({
    required this.baseUrl,
    required this.deviceName,
    this.code,
    this.password,
  });

  final String baseUrl;
  final String deviceName;
  final String? code;
  final String? password;
}

/// A [Pairer] the test drives: it records the call and answers with a token or
/// with the coded failure a wrong code produces.
Pairer fakePairer(List<PairingCall> calls, {bool reject = false}) {
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
        password: password,
      ),
    );
    if (reject) {
      throw const PiuiException(
        ErrorCodes.unauthorized,
        'wrong or expired code',
      );
    }
    return const PairResult(
      deviceId: 'd_test',
      token: 'd_test.secret',
      scope: DeviceScope.operator,
      server: ServerIdentity(
        version: '0.1.0',
        piVersion: '0.87.1',
        protocol: 1,
        features: ['sessions'],
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
        pairerProvider.overrideWithValue(fakePairer(calls, reject: reject)),
      ],
      child: const PiuiApp(),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  late MemoryProfileStore store;
  late List<PairingCall> calls;

  setUp(() {
    store = MemoryProfileStore();
    calls = [];
  });

  testWidgets('an unpaired client starts at onboarding', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    expect(find.text('Connect to a server'), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('a refused URL stays on the field with the reason', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byType(TextField), 'ftp://pi-ui.local');
    await tester.tap(find.text('Continue to pairing'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Only http and https'), findsOneWidget);
    expect(calls, isEmpty);
  });

  testWidgets('the probe reports a reachable server', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byType(TextField), 'pi-ui.local:8787');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.text('reachable'), findsOneWidget);
  });

  testWidgets('the probe reports an unreachable server', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls, reachable: false);

    await tester.enterText(find.byType(TextField), 'pi-ui.local');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.text('reachable'), findsNothing);
    expect(find.textContaining('not with "ok"'), findsOneWidget);
  });

  testWidgets('a wrong code keeps the user on the pairing screen', (
    tester,
  ) async {
    await pumpOnboarding(tester, store: store, calls: calls, reject: true);

    await tester.enterText(find.byType(TextField), 'pi-ui.local:8787');
    await tester.tap(find.text('Continue to pairing'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).at(1), '4K9M27');
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

    await tester.enterText(find.byType(TextField), 'pi-ui.local:8787');
    await tester.tap(find.text('Continue to pairing'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).at(1), '4k9m27');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(find.text('Sessions'), findsWidgets);
    expect(store.current?.token, 'd_test.secret');
    expect(store.current?.baseUrl, 'http://pi-ui.local:8787');
    expect(store.current?.scope, DeviceScope.operator);
    expect(calls.single.deviceName, isNotEmpty);
  });

  testWidgets('the admin branch pairs with the password', (tester) async {
    await pumpOnboarding(tester, store: store, calls: calls);

    await tester.enterText(find.byType(TextField), 'pi-ui.local');
    await tester.tap(find.text('Continue to pairing'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Pair with the admin password'));
    await tester.pumpAndSettle();

    // With the password branch the first field is the device name and the
    // second the secret, exactly like the code branch.
    await tester.enterText(find.byType(TextField).at(1), 'hunter2');
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();

    expect(calls.single.password, 'hunter2');
    expect(calls.single.code, isNull);
    expect(store.current?.token, 'd_test.secret');
  });
}
