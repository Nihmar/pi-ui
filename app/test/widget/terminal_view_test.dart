import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/features/terminal/terminal_view.dart';
import 'package:piui/features/terminal/vt.dart';

import '../support/test_app.dart';

/// Pumps the view over one screen.
Future<void> pumpView(WidgetTester tester, VtScreen screen) async {
  tester.view.physicalSize = const Size(700, 400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    testApp(
      home: Scaffold(
        body: SingleChildScrollView(child: TerminalView(screen: screen)),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

/// The style of the span holding [needle], so a test can assert a colour rather than a
/// screenshot.
TextStyle styleOf(WidgetTester tester, String needle) {
  final rich = tester.widget<RichText>(
    find
        .descendant(
          of: find.byType(TerminalView),
          matching: find.byType(RichText),
        )
        .first,
  );
  final root = rich.text as TextSpan;
  for (final span in root.children ?? const <InlineSpan>[]) {
    if (span is TextSpan && (span.text ?? '').contains(needle)) {
      return span.style ?? const TextStyle();
    }
  }
  fail('no span contains "$needle"');
}

void main() {
  testWidgets('the view draws the screen the emulator holds', (tester) async {
    final screen = VtScreen(columns: 20, rows: 3);
    screen.write('hello\r\nworld');
    await pumpView(tester, screen);

    // One RichText per row: the first row holds the first line.
    final rows = tester
        .widgetList<RichText>(
          find.descendant(
            of: find.byType(TerminalView),
            matching: find.byType(RichText),
          ),
        )
        .map((rich) => rich.text.toPlainText())
        .toList();
    expect(rows.first, contains('hello'));
    expect(rows[1], contains('world'));
  });

  testWidgets('a coloured cell is drawn with its colour', (tester) async {
    final screen = VtScreen(columns: 20, rows: 2);
    screen.write('\u001b[31mred\u001b[0m plain');
    await pumpView(tester, screen);

    final red = styleOf(tester, 'red');
    final plain = styleOf(tester, 'plain');
    expect(red.color, isNotNull);
    expect(plain.color, isNot(red.color));
  });

  testWidgets('bold and underline reach the span', (tester) async {
    final screen = VtScreen(columns: 20, rows: 2);
    screen.write('\u001b[1;4mfancy');
    await pumpView(tester, screen);

    final style = styleOf(tester, 'fancy');
    expect(style.fontWeight, FontWeight.w700);
    expect(style.decoration, TextDecoration.underline);
  });

  testWidgets('the cursor is drawn as a block on its own cell', (tester) async {
    final screen = VtScreen(columns: 20, rows: 2);
    screen.write('ab');
    await pumpView(tester, screen);

    // The cursor sits after 'b': the view draws it as a highlighted space.
    final rich = tester.widget<RichText>(
      find
          .descendant(
            of: find.byType(TerminalView),
            matching: find.byType(RichText),
          )
          .first,
    );
    final root = rich.text as TextSpan;
    // The reverse video of the cursor is a background colour on one of the spans.
    final hasBackground = (root.children ?? const <InlineSpan>[]).any(
      (span) => span is TextSpan && span.style?.backgroundColor != null,
    );
    expect(hasBackground, isTrue);
  });

  testWidgets('a hidden cursor leaves no block', (tester) async {
    final screen = VtScreen(columns: 20, rows: 2);
    screen.write('\u001b[?25l');
    await pumpView(tester, screen);

    final rich = tester.widget<RichText>(
      find
          .descendant(
            of: find.byType(TerminalView),
            matching: find.byType(RichText),
          )
          .first,
    );
    final root = rich.text as TextSpan;
    final hasBackground = (root.children ?? const <InlineSpan>[]).any(
      (span) => span is TextSpan && span.style?.backgroundColor != null,
    );
    expect(root.style?.backgroundColor, isNull);
    expect(hasBackground, isFalse, reason: 'a hidden cursor paints nothing');
  });
}
