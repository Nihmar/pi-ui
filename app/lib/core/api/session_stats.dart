import 'json.dart';

/// The session numbers the projection does not carry: the child answers them
/// through `get_state` and `get_session_stats`.
///
/// The protocol keeps pi's own vocabulary (`contextWindow`, `cost.total`, …), so
/// this is a thin reader, not a second model of the session.
class SessionStats {
  const SessionStats({
    this.contextTokens = 0,
    this.contextWindow = 0,
    this.messageCount = 0,
    this.pendingMessages = 0,
    this.costUsd = 0,
    this.provider,
    this.modelId,
    this.thinkingLevel,
    this.steeringMode,
    this.followUpMode,
  });

  /// Tokens currently in the context window.
  final int contextTokens;

  /// The active model's window.
  final int contextWindow;

  /// Messages pi has in the session.
  final int messageCount;

  /// Messages queued but not processed.
  final int pendingMessages;

  /// What the session has cost so far, in USD.
  final double costUsd;

  final String? provider;
  final String? modelId;
  final String? thinkingLevel;
  final String? steeringMode;
  final String? followUpMode;

  /// True when there is nothing to merge.
  bool get isEmpty =>
      contextTokens == 0 &&
      contextWindow == 0 &&
      messageCount == 0 &&
      costUsd == 0 &&
      modelId == null;

  /// Reads the `get_state` response data.
  factory SessionStats.fromState(Map<String, dynamic> data) {
    final model = asMap(data['model']);
    return SessionStats(
      messageCount: intOf(data['messageCount']),
      pendingMessages: intOf(data['pendingMessageCount']),
      contextWindow: intOf(model?['contextWindow']),
      provider: optStr(model?['provider']),
      modelId: optStr(model?['id']),
      thinkingLevel: optStr(data['thinkingLevel']),
      steeringMode: optStr(data['steeringMode']),
      followUpMode: optStr(data['followUpMode']),
    );
  }

  /// Reads the `get_session_stats` response data.
  ///
  /// pi answers with flat numbers (`cost: 0.45`, `totalMessages: 22`) and a
  /// `contextUsage` that is omitted when no model window is known.
  factory SessionStats.fromSessionStats(Map<String, dynamic> data) {
    final context = asMap(data['contextUsage']);
    return SessionStats(
      contextTokens: intOf(context?['tokens']),
      contextWindow: intOf(context?['contextWindow']),
      messageCount: intOf(data['totalMessages']),
      costUsd: doubleOf(data['cost']),
    );
  }

  /// The same numbers with [other]'s non-zero fields merged in.
  SessionStats merge(SessionStats other) => SessionStats(
    contextTokens: other.contextTokens != 0
        ? other.contextTokens
        : contextTokens,
    contextWindow: other.contextWindow != 0
        ? other.contextWindow
        : contextWindow,
    messageCount: other.messageCount != 0 ? other.messageCount : messageCount,
    pendingMessages: other.pendingMessages != 0
        ? other.pendingMessages
        : pendingMessages,
    costUsd: other.costUsd != 0 ? other.costUsd : costUsd,
    provider: other.provider ?? provider,
    modelId: other.modelId ?? modelId,
    thinkingLevel: other.thinkingLevel ?? thinkingLevel,
    steeringMode: other.steeringMode ?? steeringMode,
    followUpMode: other.followUpMode ?? followUpMode,
  );
}
