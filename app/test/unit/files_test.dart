import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/files.dart';

void main() {
  group('WorkspaceRoot', () {
    test('reads the roots envelope', () {
      final roots = WorkspaceRoot.listFrom({
        'roots': [
          {'id': 'pi-ui', 'path': '/home/u/Projects/pi-ui'},
          {'id': 'srv', 'path': '/srv'},
        ],
      });
      expect(roots.map((root) => root.id), ['pi-ui', 'srv']);
      expect(roots.first.label, 'pi-ui');
      expect(WorkspaceRoot.listFrom(null), isEmpty);
    });

    test('a root with no path to show falls back to its id', () {
      expect(const WorkspaceRoot(id: 'r', path: '/').label, 'r');
    });
  });

  group('FsEntry', () {
    test('reads a listing entry with everything the server sends', () {
      final entries = FsEntry.listFrom({
        'entries': [
          {
            'name': 'src',
            'path': '/srv/app/src',
            'rel': 'src',
            'rootId': 'app',
            'isDir': true,
            'size': 0,
            'mode': '0755',
            'modTime': '2026-09-26T12:00:00Z',
          },
          {
            'name': 'README.md',
            'path': '/srv/app/README.md',
            'rel': 'README.md',
            'rootId': 'app',
            'isDir': false,
            'size': 2048,
            'mode': '0644',
            'sha256': 'abc123',
          },
        ],
      });

      expect(entries, hasLength(2));
      expect(entries[0].isDir, isTrue);
      expect(entries[0].modTime, isNotNull);
      expect(entries[0].isMarkdown, isFalse);
      expect(entries[1].isMarkdown, isTrue);
      expect(entries[1].extension, 'md');
      expect(entries[1].sha256, 'abc123');
      expect(entries[1].size, 2048);
    });

    test('an entry without a name or a type is not a directory', () {
      final entry = FsEntry.fromJson(const {});
      expect(entry.name, '');
      expect(entry.isDir, isFalse);
      expect(entry.extension, '');
      expect(entry.isMarkdown, isFalse);
      expect(FsEntry.listFrom(const {'entries': 'nope'}), isEmpty);
    });

    test('the extension of a dotfile is empty, not the whole name', () {
      expect(
        const FsEntry(name: '.gitignore', path: '/x', isDir: false).extension,
        '',
      );
      expect(
        const FsEntry(
          name: 'archive.tar.gz',
          path: '/x',
          isDir: false,
        ).extension,
        'gz',
      );
      expect(
        const FsEntry(
          name: 'notes.MARKDOWN',
          path: '/x',
          isDir: false,
        ).isMarkdown,
        isTrue,
      );
    });
  });

  group('FileContent', () {
    test('text and entry come together', () {
      final content = FileContent.fromJson({
        'entry': {'name': 'a.md', 'path': '/a.md', 'isDir': false, 'size': 5},
        'text': 'hello',
      });
      expect(content.isBinary, isFalse);
      expect(content.text, 'hello');
      expect(content.entry.name, 'a.md');
    });

    test('base64 means there is nothing to render as text', () {
      final content = FileContent.fromJson({
        'entry': {'name': 'a.png', 'path': '/a.png', 'isDir': false},
        'base64': 'AAAA',
      });
      expect(content.isBinary, isTrue);
      expect(content.base64, 'AAAA');
      expect(content.text, isNull);
    });

    test('an empty answer is empty, not a crash', () {
      final content = FileContent.fromJson(null);
      expect(content.isBinary, isTrue);
      expect(content.entry.path, '');
    });
  });

  group('formatBytes', () {
    test('reads like a file list', () {
      expect(formatBytes(0), '0 B');
      expect(formatBytes(999), '999 B');
      expect(formatBytes(1024), '1.0 KiB');
      expect(formatBytes(1536), '1.5 KiB');
      expect(formatBytes(20 * 1024), '20 KiB');
      expect(formatBytes(3 * 1024 * 1024), '3.0 MiB');
    });
  });
}
