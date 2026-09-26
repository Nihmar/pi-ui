import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui_mockups/core/app.dart';

Future<void> pumpApp(WidgetTester tester, Size size) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(const ProviderScope(child: PiuiMockApp()));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the list shows the seeded sessions with their state', (
    tester,
  ) async {
    await pumpApp(tester, const Size(420, 900));

    expect(find.text('pi-ui'), findsOneWidget);
    expect(find.text('Niman'), findsOneWidget);
    expect(find.text('ready'), findsOneWidget);
    expect(find.text('exited'), findsOneWidget);
    expect(find.text('…/Projects/pi-ui'), findsOneWidget);
  });

  testWidgets('tapping a session opens its chat timeline', (tester) async {
    await pumpApp(tester, const Size(420, 900));

    await tester.tap(find.text('pi-ui'));
    await tester.pumpAndSettle();

    // The header and the seeded conversation are on screen; markdown is rendered
    // by the shared engine, not shown as source.
    expect(find.text('/home/user/Projects/pi-ui'), findsOneWidget);
    expect(
      find.text('Read the repository and tell me what the server does.'),
      findsOneWidget,
    );
    expect(find.textContaining('wraps the'), findsOneWidget);
    expect(find.text('List the repository root'), findsOneWidget);
  });

  testWidgets('a desktop window keeps the list next to the chat', (
    tester,
  ) async {
    await pumpApp(tester, const Size(1400, 900));

    expect(find.text('Select a session'), findsOneWidget);

    await tester.tap(find.text('Niman'));
    await tester.pumpAndSettle();

    // Master and detail are both on screen.
    expect(find.text('pi-ui'), findsOneWidget);
    expect(find.text('Add the retry to the sync queue.'), findsOneWidget);
  });

  testWidgets('creating a session opens its chat', (tester) async {
    await pumpApp(tester, const Size(420, 900));

    await tester.tap(find.byIcon(Icons.add));
    await tester.pumpAndSettle();

    expect(find.text('New session'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, '/tmp/demo');
    await tester.enterText(find.byType(TextField).last, 'demo');
    await tester.tap(find.text('Create'));
    await tester.pumpAndSettle();

    expect(find.text('demo'), findsOneWidget);
    expect(find.text('/tmp/demo'), findsOneWidget);
  });
}
