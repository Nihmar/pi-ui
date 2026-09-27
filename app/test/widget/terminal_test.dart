import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/test_app.dart';

import 'package:piui/core/api/providers.dart';
import 'package:piui/features/terminal/terminal_screen.dart';

/// Pumps the terminal with no socket: what a user sees when the link is down.
Future<void> pumpTerminal(WidgetTester tester) async {
  tester.view.physicalSize = const Size(900, 700);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [socketProvider.overrideWithValue(null)],
      child: testApp(home: const TerminalScreen(directory: '/srv/app')),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('a terminal without a connection says so', (tester) async {
    await pumpTerminal(tester);

    expect(find.text('No terminal'), findsOneWidget);
    expect(find.textContaining('Not connected'), findsOneWidget);
  });

  testWidgets('the header names the directory the shell runs in', (
    tester,
  ) async {
    await pumpTerminal(tester);

    expect(find.textContaining('/srv/app'), findsOneWidget);
  });
}
