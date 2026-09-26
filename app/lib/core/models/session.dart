/// The session projection of the mockup, mirroring `sessions.Info` of the
/// server (`schemas/core.json`, `docs/spike-interfaces.md` §5.3).
library;

/// Lifecycle state of one session.
enum SessionStatus {
  spawning,
  ready,
  streaming,
  stopping,
  exited,
  crashed;

  /// True while the session still owns a child process.
  bool get isLive =>
      this == spawning ||
      this == ready ||
      this == streaming ||
      this == stopping;

  /// The word the UI shows.
  String get label => switch (this) {
    SessionStatus.spawning => 'spawning',
    SessionStatus.ready => 'ready',
    SessionStatus.streaming => 'streaming',
    SessionStatus.stopping => 'stopping',
    SessionStatus.exited => 'exited',
    SessionStatus.crashed => 'crashed',
  };
}

/// One session as the client sees it.
class SessionModel {
  const SessionModel({
    required this.id,
    required this.cwd,
    required this.status,
    required this.createdAt,
    this.name,
    this.pid,
    this.piSessionId,
    this.modelId,
    this.provider,
    this.thinkingLevel,
    this.messageCount = 0,
    this.contextTokens = 0,
    this.contextWindow = 0,
    this.costUsd = 0,
    this.pendingMessages = 0,
    this.lastEventAt,
    this.exitCode,
  });

  /// Server session id (`s_` plus 16 hex characters in the real protocol).
  final String id;

  /// The working directory on the host.
  final String cwd;

  /// Lifecycle state.
  final SessionStatus status;

  /// When the server created the session.
  final DateTime createdAt;

  /// Display name, or null when the user never set one.
  final String? name;

  /// The child's process id while it is alive.
  final int? pid;

  /// pi's own session id.
  final String? piSessionId;

  /// Active model id.
  final String? modelId;

  /// Active provider nickname (never a URL or a key: the client only sees names).
  final String? provider;

  /// Active thinking level, in pi's own vocabulary.
  final String? thinkingLevel;

  /// Messages in the session.
  final int messageCount;

  /// Tokens currently in the context window.
  final int contextTokens;

  /// The model's context window.
  final int contextWindow;

  /// Session cost in USD.
  final double costUsd;

  /// Messages queued but not yet processed.
  final int pendingMessages;

  /// The last event the server observed for this session.
  final DateTime? lastEventAt;

  /// Exit status of a finished child.
  final int? exitCode;

  /// The name to show: the user's name, else the last path segment of the cwd.
  String get displayName {
    final explicit = name?.trim();
    if (explicit != null && explicit.isNotEmpty) {
      return explicit;
    }
    final parts = cwd.split(RegExp(r'[/\\]')).where((part) => part.isNotEmpty);
    return parts.isEmpty ? cwd : parts.last;
  }

  /// The host directory of the cwd, as `~/Projects/pi-ui` reads in a list.
  String get shortenedCwd {
    final parts = cwd
        .split(RegExp(r'[/\\]'))
        .where((part) => part.isNotEmpty)
        .toList();
    if (parts.length <= 2) {
      return cwd;
    }
    return '…/${parts.sublist(parts.length - 2).join('/')}';
  }

  /// The fraction of the context window in use, 0 when the window is unknown.
  double get contextUsage {
    if (contextWindow <= 0) {
      return 0;
    }
    return (contextTokens / contextWindow).clamp(0, 1);
  }

  /// A copy with the fields the mock scenario driver changes.
  SessionModel copyWith({
    String? cwd,
    String? name,
    SessionStatus? status,
    int? pid,
    String? piSessionId,
    String? modelId,
    String? provider,
    String? thinkingLevel,
    int? messageCount,
    int? contextTokens,
    int? contextWindow,
    double? costUsd,
    int? pendingMessages,
    DateTime? lastEventAt,
    int? exitCode,
  }) {
    return SessionModel(
      id: id,
      cwd: cwd ?? this.cwd,
      name: name ?? this.name,
      status: status ?? this.status,
      createdAt: createdAt,
      pid: pid ?? this.pid,
      piSessionId: piSessionId ?? this.piSessionId,
      modelId: modelId ?? this.modelId,
      provider: provider ?? this.provider,
      thinkingLevel: thinkingLevel ?? this.thinkingLevel,
      messageCount: messageCount ?? this.messageCount,
      contextTokens: contextTokens ?? this.contextTokens,
      contextWindow: contextWindow ?? this.contextWindow,
      costUsd: costUsd ?? this.costUsd,
      pendingMessages: pendingMessages ?? this.pendingMessages,
      lastEventAt: lastEventAt ?? this.lastEventAt,
      exitCode: exitCode ?? this.exitCode,
    );
  }
}
