import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui_markdown/piui_markdown.dart';

void main() {
  Future<void> pumpView(
    WidgetTester tester,
    String data, {
    bool selectable = true,
    ValueChanged<String>? onLinkTap,
  }) {
    return tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MarkdownView(
            data,
            selectable: selectable,
            onLinkTap: onLinkTap,
          ),
        ),
      ),
    );
  }

  testWidgets('renders prose, headings and fenced code', (tester) async {
    await pumpView(
      tester,
      '# Title\n\nbody text\n\n```dart\nvoid main() {}\n```',
    );

    expect(find.text('Title'), findsOneWidget);
    expect(find.text('body text'), findsOneWidget);
    expect(find.text('void main() {}'), findsOneWidget);
    expect(find.text('dart'), findsOneWidget);
  });

  testWidgets('renders list markers and quote borders', (tester) async {
    await pumpView(tester, '- one\n- two\n\n> quoted');

    expect(find.text('•'), findsNWidgets(2));
    expect(find.text('one'), findsOneWidget);
    expect(find.text('quoted'), findsOneWidget);
  });

  testWidgets('a link is tappable when a handler is given', (tester) async {
    final tapped = <String>[];
    await pumpView(
      tester,
      'see [the docs](https://x.example)',
      onLinkTap: tapped.add,
    );

    final selectable = tester.widget<SelectableText>(
      find.byType(SelectableText),
    );
    final recognizer = _linkRecognizerOf(selectable.textSpan!);
    expect(recognizer, isNotNull);
    recognizer!.onTap!();
    expect(tapped, ['https://x.example']);
  });

  testWidgets('links keep their style but are inert without a handler', (
    tester,
  ) async {
    await pumpView(tester, 'see [the docs](https://x.example)');

    final selectable = tester.widget<SelectableText>(
      find.byType(SelectableText),
    );
    expect(_linkRecognizerOf(selectable.textSpan!), isNull);
    expect(
      _underlinedSpanOf(selectable.textSpan!)!.style?.decoration,
      TextDecoration.underline,
    );
  });

  testWidgets('selectable: false renders plain text', (tester) async {
    await pumpView(tester, 'body', selectable: false);

    expect(find.byType(SelectableText), findsNothing);
    expect(find.text('body'), findsOneWidget);
  });

  testWidgets('an empty document renders nothing', (tester) async {
    await pumpView(tester, '');

    expect(find.byType(SelectableText), findsNothing);
  });
}

/// Finds the recognizer of the first link span in a span tree.
TapGestureRecognizer? _linkRecognizerOf(InlineSpan span) {
  final link = _linkSpanOf(span);
  final recognizer = link?.recognizer;
  return recognizer is TapGestureRecognizer ? recognizer : null;
}

/// Finds the first link span (a span with a recognizer) in a span tree.
TextSpan? _linkSpanOf(InlineSpan span) {
  if (span is! TextSpan) {
    return null;
  }
  if (span.recognizer != null) {
    return span;
  }
  for (final child in span.children ?? const <InlineSpan>[]) {
    final found = _linkSpanOf(child);
    if (found != null) {
      return found;
    }
  }
  return null;
}

/// Finds the first span styled as a link in a span tree.
TextSpan? _underlinedSpanOf(InlineSpan span) {
  if (span is! TextSpan) {
    return null;
  }
  if (span.style?.decoration == TextDecoration.underline) {
    return span;
  }
  for (final child in span.children ?? const <InlineSpan>[]) {
    final found = _underlinedSpanOf(child);
    if (found != null) {
      return found;
    }
  }
  return null;
}
