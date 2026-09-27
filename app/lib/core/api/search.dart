import 'json.dart';

/// One search result, wherever it came from.
///
/// `kind` is `file` (a ripgrep hit inside a workspace) or `message` (a line of a pi
/// session). The two share a shape on purpose: a result list that mixes them stays one
/// list, and only the action a tap performs depends on the kind.
class SearchHit {
  const SearchHit({
    required this.kind,
    required this.path,
    required this.text,
    this.rel = '',
    this.rootId = '',
    this.line = 0,
    this.column = 0,
    this.sessionId,
    this.role,
    this.at,
  });

  /// `file` or `message`.
  final String kind;

  /// The file the hit is in: a host path for both kinds (a session file for a message).
  final String path;

  /// The matching line, or an excerpt around the match.
  final String text;

  /// The path relative to the workspace root, for a file hit.
  final String rel;

  /// The workspace the file hit belongs to.
  final String rootId;

  /// 1-based line and column inside the file.
  final int line;
  final int column;

  /// The session a message hit belongs to.
  final String? sessionId;

  /// The role of the message (`user`, `assistant`).
  final String? role;

  /// When the message was written.
  final DateTime? at;

  /// True when the hit is a conversation line rather than a file line.
  bool get isMessage => kind == 'message';

  /// The name a result row shows: the file name, or the session id.
  String get title {
    if (isMessage) {
      return sessionId ?? path;
    }
    if (rel.isNotEmpty) {
      return rel;
    }
    final parts = path.split('/').where((part) => part.isNotEmpty).toList();
    return parts.isEmpty ? path : parts.last;
  }

  /// The line a result row shows under the title.
  String get location => isMessage
      ? '${role ?? 'message'}${at == null ? '' : ' · ${_shortTime(at!)}'}'
      : '$path:$line';

  /// Reads one hit out of `GET /search`.
  factory SearchHit.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return SearchHit(
      kind: str(json['kind'], fallback: 'file'),
      path: str(json['path']),
      text: str(json['text']),
      rel: str(json['rel']),
      rootId: str(json['rootId']),
      line: intOf(json['line']),
      column: intOf(json['column']),
      sessionId: optStr(json['sessionId']),
      role: optStr(json['role']),
      at: timeOf(json['at']),
    );
  }

  /// Reads the `{"hits":[…]}` envelope.
  static List<SearchHit> listFrom(Object? body) => [
    for (final entry in asMapList(asMap(body)?['hits']))
      SearchHit.fromJson(entry),
  ];

  static String _shortTime(DateTime at) {
    final local = at.toLocal();
    return '${local.year}-${_two(local.month)}-${_two(local.day)}';
  }

  static String _two(int value) => value.toString().padLeft(2, '0');
}

/// What a search asks for: the text, which halves, and where to look.
///
/// It is a value so a provider can watch it: two searches that differ only in whitespace
/// are the same search.
typedef SearchRequest = ({String query, List<String> scope, String? cwd});
