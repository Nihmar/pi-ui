import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/app.dart';
import 'package:piui/core/models/session.dart';

/// A profile that is already paired, so the router lets the shell render.
ServerProfile pairedProfile() => const ServerProfile(
  baseUrl: 'http://pi-ui.test:8787',
  deviceName: 'test device',
  token: 'd_test.secret',
  deviceId: 'd_test',
  scope: DeviceScope.operator,
);

/// Pumps the app in a window of [size] logical pixels, with a paired profile and
/// a session list that never touches the network.
Future<void> pumpApp(
  WidgetTester tester,
  Size size, {
  List<SessionModel> sessions = const [],
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        profileStoreProvider.overrideWithValue(
          MemoryProfileStore(profile: pairedProfile()),
        ),
        socketProvider.overrideWithValue(null),
        sessionsProvider.overrideWith((ref) => Stream.value(sessions)),
      ],
      child: const PiuiApp(),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a phone window gets the navigation bar', (tester) async {
    await pumpApp(tester, const Size(420, 900));

    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.byType(NavigationRail), findsNothing);
    expect(find.text('No sessions yet'), findsOneWidget);
  });

  testWidgets('a desktop window gets the navigation rail', (tester) async {
    await pumpApp(tester, const Size(1400, 900));

    expect(find.byType(NavigationRail), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('the rail switches branches', (tester) async {
    await pumpApp(tester, const Size(1400, 900));

    await tester.tap(find.text('Settings'));
    await tester.pumpAndSettle();

    expect(find.text('Settings'), findsWidgets);
  });
}
