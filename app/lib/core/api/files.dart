import 'json.dart';

/// One workspace the server allows: the root a client may browse.
///
/// The paths are the host's, not the server's: `internal/fs` is what decides whether a
/// path is inside a root, and the client only ever echoes what it was given.
class WorkspaceRoot {
  const WorkspaceRoot({required this.id, required this.path});

  /// The short name a listing reports as `rootId`.
  final String id;

  /// The absolute directory on the host.
  final String path;

  /// The name a list shows: the last segment of the path, or the id.
  String get label {
    final parts = path
        .split(RegExp(r'[/\\]'))
        .where((part) => part.isNotEmpty)
        .toList();
    return parts.isEmpty ? id : parts.last;
  }

  /// Reads one root out of `GET /workspaces`.
  factory WorkspaceRoot.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return WorkspaceRoot(id: str(json['id']), path: str(json['path']));
  }

  /// Reads the whole `{"roots":[…]}` envelope.
  static List<WorkspaceRoot> listFrom(Object? body) => [
    for (final entry in asMapList(asMap(body)?['roots']))
      WorkspaceRoot.fromJson(entry),
  ];
}

/// One file or directory, as a listing reports it.
class FsEntry {
  const FsEntry({
    required this.name,
    required this.path,
    required this.isDir,
    this.rel = '',
    this.rootId = '',
    this.size = 0,
    this.mode = '',
    this.modTime,
    this.sha256,
  });

  /// The last path segment.
  final String name;

  /// The absolute path on the host, which is what a request sends back.
  final String path;

  /// True for a directory.
  final bool isDir;

  /// The path relative to the root that contains it.
  final String rel;

  /// The root this entry belongs to.
  final String rootId;

  /// Size in bytes (0 for a directory).
  final int size;

  /// Permission bits as octal text (`0644`).
  final String mode;

  /// Last modification time, when the server reported one.
  final DateTime? modTime;

  /// Content hash, present for files the server read back.
  final String? sha256;

  /// The lower-case extension without the dot, for choosing a viewer.
  String get extension {
    final index = name.lastIndexOf('.');
    if (index <= 0 || index == name.length - 1) {
      return '';
    }
    return name.substring(index + 1).toLowerCase();
  }

  /// True when the markdown engine should render this file.
  bool get isMarkdown =>
      const {'md', 'markdown', 'mdown', 'mkd'}.contains(extension);

  /// Reads one entry out of a listing.
  factory FsEntry.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return FsEntry(
      name: str(json['name']),
      path: str(json['path']),
      isDir: boolOf(json['isDir']),
      rel: str(json['rel']),
      rootId: str(json['rootId']),
      size: intOf(json['size']),
      mode: str(json['mode']),
      modTime: timeOf(json['modTime']),
      sha256: optStr(json['sha256']),
    );
  }

  /// Reads the `{"entries":[…]}` envelope of `GET /fs/list`.
  static List<FsEntry> listFrom(Object? body) => [
    for (final entry in asMapList(asMap(body)?['entries']))
      FsEntry.fromJson(entry),
  ];
}

/// The content of one file: text when it is valid UTF-8, base64 when it is not.
///
/// The server decides which of the two, because it is the one that saw the bytes: a
/// client that guessed would corrupt what it cannot render.
class FileContent {
  const FileContent({required this.entry, this.text, this.base64});

  /// The metadata of what was read.
  final FsEntry entry;

  /// The decoded text, when the content is valid UTF-8.
  final String? text;

  /// The base64 payload, when it is not.
  final String? base64;

  /// True when there is nothing to render as text.
  bool get isBinary => text == null;

  /// Reads a `GET /files/read` answer.
  factory FileContent.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return FileContent(
      entry: FsEntry.fromJson(json['entry']),
      text: json['text'] is String ? json['text'] as String : null,
      base64: optStr(json['base64']),
    );
  }
}

/// Formats a byte count the way a file list reads it.
String formatBytes(int bytes) {
  if (bytes < 1024) {
    return '$bytes B';
  }
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  var value = bytes / 1024;
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  final text = value >= 10
      ? value.toStringAsFixed(0)
      : value.toStringAsFixed(1);
  return '$text ${units[unit]}';
}
