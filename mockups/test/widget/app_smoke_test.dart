import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui_mockups/core/app.dart';

/// Pumps the app in a window of [size] logical pixels.
Future<void> pumpApp(WidgetTester tester, Size size) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(const ProviderScope(child: PiuiMockApp()));
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

    expect(find.text('Nothing to configure yet'), findsOneWidget);
  });
}
