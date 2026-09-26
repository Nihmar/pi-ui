import '../models/chat_entry.dart';
import 'json.dart';

/// One path git reports as changed.
class GitChange {
  const GitChange({
    required this.path,
    required this.status,
    this.staged = false,
    this.unstaged = false,
  });

  /// The path relative to the repository.
  final String path;

  /// The porcelain code (`M`, `A`, `??`, …).
  final String status;

  /// True when the index side of the code is dirty.
  final bool staged;

  /// True when the working tree side is dirty.
  final bool unstaged;

  /// The word a list shows for this change.
  String get label => switch (status) {
    '??' => 'untracked',
    'M' => 'modified',
    'A' => 'added',
    'D' => 'deleted',
    'R' => 'renamed',
    'C' => 'copied',
    'U' => 'conflict',
    _ => status.isEmpty ? 'changed' : status,
  };

  /// The file name a row shows.
  String get name {
    final parts = path.split('/').where((part) => part.isNotEmpty).toList();
    return parts.isEmpty ? path : parts.last;
  }

  /// Reads one change of a `git status`.
  factory GitChange.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return GitChange(
      path: str(json['path']),
      status: str(json['status']),
      staged: boolOf(json['staged']),
      unstaged: boolOf(json['unstaged']),
    );
  }
}

/// The working tree of one repository, as `GET /git/status` reports it.
class GitStatus {
  const GitStatus({
    required this.repo,
    this.branch = 'HEAD',
    this.detached = false,
    this.ahead = 0,
    this.behind = 0,
    this.clean = true,
    this.changes = const [],
  });

  /// The directory git ran in.
  final String repo;

  /// The checked-out branch, or `HEAD` when detached.
  final String branch;

  /// True when no branch is checked out.
  final bool detached;

  /// Commits the branch is ahead of its upstream.
  final int ahead;

  /// Commits it is behind.
  final int behind;

  /// True when there is nothing to stage or commit.
  final bool clean;

  /// Every dirty path, staged and unstaged together.
  final List<GitChange> changes;

  /// The changes that are staged, which is what a commit records.
  List<GitChange> get stagedChanges => [
    for (final change in changes)
      if (change.staged) change,
  ];

  /// True when a commit would record something.
  bool get canCommit => stagedChanges.isNotEmpty;

  /// Reads a `GET /git/status` answer.
  factory GitStatus.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return GitStatus(
      repo: str(json['repo']),
      branch: str(json['branch'], fallback: 'HEAD'),
      detached: boolOf(json['detached']),
      ahead: intOf(json['ahead']),
      behind: intOf(json['behind']),
      // A server that reports neither `clean` nor changes is clean: assuming a dirty
      // tree with nothing to show would offer a commit with nothing to record.
      clean: json.containsKey('clean')
          ? boolOf(json['clean'])
          : asList(json['changes']).isEmpty,
      changes: [
        for (final change in asList(json['changes']))
          GitChange.fromJson(change),
      ],
    );
  }
}

/// One entry of `GET /git/log`.
class GitCommit {
  const GitCommit({
    required this.hash,
    required this.short,
    required this.subject,
    this.author = '',
    this.at,
  });

  final String hash;
  final String short;
  final String subject;
  final String author;
  final DateTime? at;

  /// Reads one commit.
  factory GitCommit.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return GitCommit(
      hash: str(json['hash']),
      short: str(json['short']),
      subject: str(json['subject']),
      author: str(json['author']),
      at: timeOf(json['at']),
    );
  }

  /// Reads the `{"commits":[…]}` envelope.
  static List<GitCommit> listFrom(Object? body) => [
    for (final entry in asMapList(asMap(body)?['commits']))
      GitCommit.fromJson(entry),
  ];
}

/// Maps a unified diff onto the timeline's diff model, so the chat's own renderer shows
/// it instead of a second implementation.
///
/// It reads the `+++`/`---` header for the path, counts the added and removed lines and
/// keeps the hunk lines in order; anything it does not recognise (a binary notice, a mode
/// change) travels as context, because a diff that hides what it did not understand is
/// worse than a diff that shows a strange line.
DiffPreview diffPreviewFrom(String path, String diff) {
  final lines = <DiffLine>[];
  var added = 0;
  var removed = 0;
  var currentPath = path;
  for (final raw in diff.split('\n')) {
    if (raw.startsWith('diff --git ')) {
      continue;
    }
    if (raw.startsWith('+++ ')) {
      final target = raw.substring(4).trim();
      if (target != '/dev/null') {
        currentPath = target.startsWith('b/') ? target.substring(2) : target;
      }
      continue;
    }
    if (raw.startsWith('--- ') ||
        raw.startsWith('index ') ||
        raw.startsWith('@@')) {
      continue;
    }
    if (raw.startsWith('+')) {
      added++;
      lines.add(DiffLine(DiffLineKind.added, raw.substring(1)));
      continue;
    }
    if (raw.startsWith('-')) {
      removed++;
      lines.add(DiffLine(DiffLineKind.removed, raw.substring(1)));
      continue;
    }
    if (raw.isEmpty) {
      continue;
    }
    lines.add(
      DiffLine(
        DiffLineKind.context,
        raw.startsWith(' ') ? raw.substring(1) : raw,
      ),
    );
  }
  return DiffPreview(
    path: currentPath,
    lines: lines,
    added: added,
    removed: removed,
  );
}
