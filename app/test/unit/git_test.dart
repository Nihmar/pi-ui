import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/git.dart';
import 'package:piui/core/models/chat_entry.dart';

void main() {
  group('GitStatus', () {
    test('reads the projection with its changes', () {
      final status = GitStatus.fromJson({
        'repo': '/srv/app',
        'branch': 'main',
        'detached': false,
        'ahead': 2,
        'behind': 1,
        'clean': false,
        'changes': [
          {
            'path': 'lib/main.dart',
            'status': 'M',
            'staged': true,
            'unstaged': false,
          },
          {
            'path': 'notes.md',
            'status': '??',
            'staged': false,
            'unstaged': true,
          },
        ],
      });

      expect(status.branch, 'main');
      expect(status.ahead, 2);
      expect(status.behind, 1);
      expect(status.clean, isFalse);
      expect(status.stagedChanges.single.path, 'lib/main.dart');
      expect(status.canCommit, isTrue);
      expect(status.changes[1].label, 'untracked');
      expect(status.changes[1].name, 'notes.md');
    });

    test('an empty answer is a clean HEAD', () {
      final status = GitStatus.fromJson(const {});
      expect(status.branch, 'HEAD');
      expect(status.clean, isTrue);
      expect(status.canCommit, isFalse);
      expect(status.changes, isEmpty);
    });

    test('a change without a status reads as changed', () {
      const change = GitChange(path: 'a/b/c.txt', status: '');
      expect(change.label, 'changed');
      expect(change.name, 'c.txt');
    });
  });

  group('GitCommit', () {
    test('reads the log envelope', () {
      final commits = GitCommit.listFrom({
        'commits': [
          {
            'hash': 'abc123',
            'short': 'abc',
            'subject': 'first',
            'author': 'Alessandro',
            'at': '2026-09-26T12:00:00Z',
          },
        ],
      });
      expect(commits.single.short, 'abc');
      expect(commits.single.author, 'Alessandro');
      expect(commits.single.at, isNotNull);
      expect(GitCommit.listFrom(null), isEmpty);
    });
  });

  group('diffPreviewFrom', () {
    test('reads a unified diff into the timeline model', () {
      const diff = '''
diff --git a/lib/main.dart b/lib/main.dart
index 111..222 100644
--- a/lib/main.dart
+++ b/lib/main.dart
@@ -1,3 +1,3 @@
 void main() {
-  print('old');
+  print('new');
 }
''';
      final preview = diffPreviewFrom('lib/main.dart', diff);
      expect(preview.path, 'lib/main.dart');
      expect(preview.added, 1);
      expect(preview.removed, 1);
      expect(preview.lines.map((line) => line.kind), [
        DiffLineKind.context,
        DiffLineKind.removed,
        DiffLineKind.added,
        DiffLineKind.context,
      ]);
      expect(preview.lines[2].text, "  print('new');");
    });

    test('a header for a file that does not exist keeps the asked path', () {
      final preview = diffPreviewFrom('gone.txt', '''
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
''');
      expect(preview.path, 'gone.txt');
      expect(preview.removed, 1);
    });

    test('an empty diff is an empty preview, not a crash', () {
      final preview = diffPreviewFrom('a.txt', '');
      expect(preview.lines, isEmpty);
      expect(preview.added, 0);
      expect(preview.removed, 0);
    });
  });
}
