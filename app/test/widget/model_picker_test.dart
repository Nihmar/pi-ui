import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/test_app.dart';

import 'package:piui/core/api/models.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/features/chat/widgets/model_picker.dart';

/// Pumps the picker with a fixed model list: the socket is not what this test is about.
Future<void> pumpPicker(
  WidgetTester tester, {
  required List<ModelOption> models,
  bool failing = false,
}) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        socketProvider.overrideWithValue(null),
        sessionModelsProvider.overrideWith((ref, sessionId) async {
          if (failing) {
            throw Exception('pi is unreachable');
          }
          return models;
        }),
      ],
      // The real theme, because the widgets read their colors from its tokens.
      child: testApp(
        home: const Scaffold(body: ModelPicker(sessionId: 's_1')),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the picker lists what pi offers', (tester) async {
    await pumpPicker(
      tester,
      models: const [
        ModelOption(
          provider: 'llama.cpp',
          id: 'qwen3-coder',
          name: 'Qwen3 Coder',
          contextWindow: 128000,
          reasoning: true,
        ),
        ModelOption(provider: 'anthropic', id: 'claude'),
      ],
    );

    expect(find.text('Qwen3 Coder'), findsOneWidget);
    expect(find.text('llama.cpp · 128000 tokens'), findsOneWidget);
    expect(find.text('claude'), findsOneWidget);
    expect(find.text('anthropic'), findsOneWidget);
  });

  testWidgets('an empty catalogue says so instead of showing nothing', (
    tester,
  ) async {
    await pumpPicker(tester, models: const []);

    expect(find.textContaining('reported no model'), findsOneWidget);
  });

  testWidgets('a failed load is an error the picker shows', (tester) async {
    await pumpPicker(tester, models: const [], failing: true);

    expect(find.textContaining('could not be read'), findsOneWidget);
  });

  testWidgets('the reasoning levels are offered', (tester) async {
    await pumpPicker(
      tester,
      models: const [ModelOption(provider: 'p', id: 'm')],
    );

    expect(find.text('off'), findsOneWidget);
    expect(find.text('high'), findsOneWidget);
  });
}
