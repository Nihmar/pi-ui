import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/files.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/theme/app_theme.dart';
import 'package:piui/features/files/file_browser_screen.dart';
import 'package:piui/features/files/file_viewer_screen.dart';

/// The two directories the fake server knows.
const _root = '/srv/app';
const _sub = '/srv/app/lib';

/// A listing that answers per path, the way the real provider does.
List<FsEntry> entriesOf(String path) => switch (path) {
  _root => const [
    FsEntry(name: 'lib', path: _sub, isDir: true, mode: '0755'),
    FsEntry(
      name: 'README.md',
      path: '$_root/README.md',
      isDir: false,
      size: 40,
    ),
  ],
  _sub => const [
    FsEntry(
      name: 'main.dart',
      path: '$_sub/main.dart',
      isDir: false,
      size: 120,
    ),
    FsEntry(
      name: '.gitignore',
      path: '$_sub/.gitignore',
      isDir: false,
      size: 12,
    ),
  ],
  _ => const [],
};

FileContent contentOf(String path) => path.endsWith('.md')
    ? const FileContent(
        entry: FsEntry(
          name: 'README.md',
          path: '$_root/README.md',
          isDir: false,
          size: 40,
        ),
        text: '# pi-ui\n\nA *readme* from the host.',
      )
    : const FileContent(
        entry: FsEntry(
          name: 'main.dart',
          path: '$_sub/main.dart',
          isDir: false,
          size: 120,
        ),
        text: 'void main() {}',
      );

/// Pumps one file screen with the filesystem stubbed.
Future<void> pumpScreen(
  WidgetTester tester,
  Widget screen, {
  List<WorkspaceRoot> roots = const [WorkspaceRoot(id: 'app', path: _root)],
  bool expanded = false,
}) async {
  tester.view.physicalSize = expanded
      ? const Size(1400, 900)
      : const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        socketProvider.overrideWithValue(null),
        workspacesProvider.overrideWith((ref) async => roots),
        directoryProvider.overrideWith((ref, path) async => entriesOf(path)),
        fileContentProvider.overrideWith((ref, path) async => contentOf(path)),
      ],
      child: MaterialApp(
        theme: AppTheme.dark(),
        home: Scaffold(body: screen),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('the browser lists a workspace', (tester) async {
    await pumpScreen(tester, const FileBrowserScreen());

    expect(find.text('lib'), findsOneWidget);
    expect(find.text('README.md'), findsOneWidget);
    expect(find.text('40 B'), findsOneWidget);
  });

  testWidgets('a directory becomes the breadcrumb', (tester) async {
    await pumpScreen(tester, const FileBrowserScreen(initialPath: _sub));

    // The crumb trail names the root and the directory, and the listing is the sub one.
    expect(find.text('app'), findsOneWidget);
    expect(find.text('main.dart'), findsOneWidget);
    expect(find.text('.gitignore'), findsOneWidget);
    expect(find.text('README.md'), findsNothing);
  });

  testWidgets('a server without a workspace says so', (tester) async {
    await pumpScreen(tester, const FileBrowserScreen(), roots: const []);

    expect(find.text('No workspace'), findsOneWidget);
    expect(find.textContaining('--root'), findsOneWidget);
  });

  testWidgets('a markdown file renders through the shared engine', (
    tester,
  ) async {
    await pumpScreen(tester, const FileViewerScreen(path: '$_root/README.md'));

    expect(find.text('pi-ui'), findsWidgets);
    expect(find.textContaining('readme'), findsWidgets);
    expect(find.text('sha256 40 B'), findsNothing);
  });

  testWidgets('a text file is monospace, not markdown', (tester) async {
    await pumpScreen(tester, const FileViewerScreen(path: '$_sub/main.dart'));

    expect(find.text('void main() {}'), findsOneWidget);
  });

  testWidgets('the desktop layout shows the listing beside the file', (
    tester,
  ) async {
    await pumpScreen(
      tester,
      const FileDetailScreen(path: '$_root/README.md'),
      expanded: true,
    );

    // The master pane lists the directory the file lives in, not the root.
    expect(
      find.text('lib'),
      findsOneWidget,
      reason: 'the master pane lists the directory of the open file',
    );
    expect(find.text('README.md'), findsWidgets);
    expect(find.byType(FileViewerScreen), findsOneWidget);
  });
}
