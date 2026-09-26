import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui_mockups/core/app.dart';

/// Opens the app on a phone window, opens the first seeded session and returns.
Future<void> openSession(WidgetTester tester) async {
  // A tall phone window: the whole seeded timeline is on screen, so an
  // assertion never depends on ListView building an off-screen item.
  tester.view.physicalSize = const Size(420, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(const ProviderScope(child: PiuiMockApp()));
  await tester.pumpAndSettle();
  await tester.tap(find.text('pi-ui'));
  await tester.pumpAndSettle();
}

/// Runs one scenario from the header's scenario sheet.
Future<void> runScenario(WidgetTester tester, String label) async {
  await tester.tap(find.byIcon(Icons.play_circle_outline));
  await tester.pumpAndSettle();
  final entry = find.text(label);
  if (entry.evaluate().isEmpty) {
    // The sheet scrolls: a scenario below the fold is scrolled into view.
    await tester.scrollUntilVisible(
      entry,
      120,
      scrollable: find.byType(Scrollable).last,
    );
  }
  await tester.tap(entry);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('sending a prompt starts a streaming answer', (tester) async {
    await openSession(tester);

    await tester.enterText(find.byType(TextField), 'Explain the replay path');
    await tester.tap(find.text('Send'));
    await tester.pump(const Duration(milliseconds: 80));

    expect(find.text('Explain the replay path'), findsOneWidget);
    expect(find.text('Session is streaming'), findsOneWidget);
    // While the session streams the primary action is steer: a plain prompt
    // would come back as busy_streaming.
    expect(find.text('Steer'), findsOneWidget);
    expect(find.text('Queue'), findsNothing);

    // Let the scripted answer finish so the run leaves no timer behind.
    await tester.pumpAndSettle(const Duration(milliseconds: 100));
  });

  testWidgets('a steer queues and can be cancelled', (tester) async {
    await openSession(tester);
    await tester.enterText(find.byType(TextField), 'First prompt');
    await tester.tap(find.text('Send'));
    await tester.pump(const Duration(milliseconds: 80));

    await tester.enterText(find.byType(TextField), 'Actually, check the tests');
    await tester.tap(find.text('Steer'));
    await tester.pumpAndSettle();

    expect(find.text('1 message waiting'), findsOneWidget);
    expect(find.text('Actually, check the tests'), findsOneWidget);

    await tester.tap(find.byIcon(Icons.close));
    await tester.pumpAndSettle();
    expect(find.text('1 message waiting'), findsNothing);

    await tester.pumpAndSettle(const Duration(milliseconds: 100));
  });

  testWidgets('offline messages queue and flush on reconnect', (tester) async {
    await openSession(tester);
    await runScenario(tester, 'Offline queue');

    expect(find.textContaining('messages are queued'), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'Queued while offline');
    await tester.tap(find.text('Send'));
    await tester.pumpAndSettle();
    expect(find.text('queued offline'), findsOneWidget);

    await tester.tap(find.text('Reconnect'));
    await tester.pumpAndSettle(const Duration(milliseconds: 500));

    expect(find.textContaining('messages are queued'), findsNothing);
    expect(find.textContaining('queued message'), findsOneWidget);
    expect(find.text('queued offline'), findsNothing);
    await tester.pumpAndSettle(const Duration(milliseconds: 100));
  });

  testWidgets('the approval dialog answers and disappears', (tester) async {
    await openSession(tester);
    await runScenario(tester, 'Approval dialog');

    expect(find.text('Run bash command?'), findsOneWidget);
    expect(find.textContaining('rm -rf build/'), findsOneWidget);

    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();

    expect(find.text('Run bash command?'), findsNothing);
    expect(find.textContaining('approved'), findsOneWidget);
  });
}
