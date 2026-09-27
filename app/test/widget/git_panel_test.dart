import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/test_app.dart';

import 'package:piui/core/api/git.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/models/chat_entry.dart';
import 'package:piui/features/git/git_panel.dart';

/// A repository with one staged and one untracked path.
const _dirty = GitStatus(
  repo: '/srv/app',
  branch: 'main',
  ahead: 1,
  changes: [
    GitChange(path: 'lib/main.dart', status: 'M', staged: true, unstaged: true),
    GitChange(path: 'notes.md', status: '??', unstaged: true),
  ],
  clean: false,
);

/// Pumps the panel with a stubbed repository.
Future<void> pumpPanel(
  WidgetTester tester, {
  GitStatus status = _dirty,
  bool failDiff = false,
}) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        socketProvider.overrideWithValue(null),
        gitStatusProvider.overrideWith((ref, dir) async => status),
        gitLogProvider.overrideWith((ref, dir) async => const <GitCommit>[]),
        gitDiffProvider.overrideWith((ref, request) async {
          if (failDiff) {
            throw Exception('the diff is unavailable');
          }
          return DiffPreview(
            path: request.path,
            lines: const [DiffLine(DiffLineKind.added, 'new line')],
            added: 1,
          );
        }),
      ],
      child: testApp(
        home: const Scaffold(body: GitPanel(directory: '/srv/app')),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the panel shows the branch and the changes', (tester) async {
    await pumpPanel(tester);

    expect(find.text('main · ↑1'), findsOneWidget);
    expect(find.text('/srv/app'), findsOneWidget);
    expect(find.text('main.dart'), findsOneWidget);
    expect(find.text('modified'), findsOneWidget);
    expect(find.text('untracked'), findsOneWidget);
    // Only the untracked path can be staged: the other one already is.
    expect(find.byTooltip('Stage this path'), findsOneWidget);
  });

  testWidgets('selecting a change shows its diff through the chat renderer', (
    tester,
  ) async {
    await pumpPanel(tester);

    expect(find.text('Select a change to see its diff.'), findsOneWidget);
    // The path appears twice per row (name and full path), so the row is what is tapped.
    await tester.tap(find.text('notes.md').first);
    // Two pumps instead of pumpAndSettle: the diff resolves in a microtask, and a
    // settle would wait on the loading spinner's animation.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('new line'), findsOneWidget);
  });

  test('a failed diff propagates as an error, not as an empty diff', () async {
    // The panel's diff area is a plain `AsyncValue.when`, so what this pins is the
    // contract underneath it: a failure reaches the collector instead of turning into an
    // empty preview that would read as "no change".
    final container = ProviderContainer(
      overrides: [
        socketProvider.overrideWithValue(null),
        gitDiffProvider.overrideWith(
          (ref, request) =>
              Future<DiffPreview>.error(Exception('the diff is unavailable')),
        ),
      ],
    );
    addTearDown(container.dispose);

    final values = <AsyncValue<DiffPreview>>[];
    container.listen(
      gitDiffProvider(const (dir: '/srv/app', path: 'notes.md', staged: false)),
      (_, next) => values.add(next),
    );
    await Future<void>.delayed(Duration.zero);

    expect(values, isNotEmpty);
    expect(
      values.last.hasError,
      isTrue,
      reason: 'the failure must not become an empty diff',
    );
  });

  testWidgets('a clean tree says there is nothing to commit', (tester) async {
    await pumpPanel(
      tester,
      status: const GitStatus(repo: '/srv/app', branch: 'main'),
    );

    expect(find.textContaining('Nothing to commit'), findsOneWidget);
  });

  testWidgets('a commit needs a message', (tester) async {
    await pumpPanel(tester);

    await tester.tap(find.text('Commit'));
    await tester.pumpAndSettle();

    expect(find.text('A commit needs a message.'), findsOneWidget);
  });
}
