import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/test_app.dart';

import 'package:piui/core/api/providers.dart';
import 'package:piui/core/api/search.dart';
import 'package:piui/features/search/search_screen.dart';

/// Pumps the search screen with results from a table keyed by the query.
Future<void> pumpSearch(
  WidgetTester tester, {
  required Map<String, List<SearchHit>> results,
  String query = '',
}) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        socketProvider.overrideWithValue(null),
        searchResultsProvider.overrideWith((ref) async {
          final request = ref.watch(searchRequestProvider);
          return results[request.query.trim()] ?? const <SearchHit>[];
        }),
      ],
      child: testApp(home: const SearchScreen()),
    ),
  );
  await tester.pumpAndSettle();
  if (query.isNotEmpty) {
    await tester.enterText(find.byType(TextField), query);
    // The debounce is what makes a per-keystroke request impossible.
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();
  }
}

void main() {
  testWidgets('a short query asks for nothing', (tester) async {
    await pumpSearch(tester, results: const {});

    expect(find.text('Search the host'), findsOneWidget);
    expect(find.text('No match'), findsNothing);
  });

  testWidgets('the results show files and messages in one list', (
    tester,
  ) async {
    await pumpSearch(
      tester,
      query: 'rpc',
      results: {
        'rpc': const [
          SearchHit(
            kind: 'file',
            path: '/srv/app/lib/rpc.dart',
            rel: 'lib/rpc.dart',
            line: 3,
            text: 'the RpcBridge',
          ),
          SearchHit(
            kind: 'message',
            path: '/home/u/.pi/sessions/x.jsonl',
            text: 'the framing of RpcBridge',
            sessionId: 's_1',
            role: 'assistant',
          ),
        ],
      },
    );

    expect(find.text('lib/rpc.dart'), findsOneWidget);
    expect(find.text('s_1'), findsOneWidget);
    expect(find.text('the RpcBridge'), findsOneWidget);
    expect(find.textContaining('assistant'), findsOneWidget);
  });

  testWidgets('no match says so', (tester) async {
    await pumpSearch(tester, query: 'nothing-here', results: const {});

    expect(find.text('No match'), findsOneWidget);
  });

  testWidgets('the scopes are toggles', (tester) async {
    await pumpSearch(tester, results: const {});

    final files = tester.widget<FilterChip>(
      find.widgetWithText(FilterChip, 'files'),
    );
    expect(files.selected, isTrue);

    await tester.tap(find.widgetWithText(FilterChip, 'files'));
    await tester.pumpAndSettle();
    final deselected = tester.widget<FilterChip>(
      find.widgetWithText(FilterChip, 'files'),
    );
    expect(deselected.selected, isFalse);
  });
}
