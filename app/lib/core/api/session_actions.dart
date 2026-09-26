import '../models/session.dart';
import 'client.dart';
import 'models.dart';
import 'socket.dart';

/// The operations the UI performs on one session.
///
/// Every driving op goes through the socket (a prompt is a command frame, not a
/// REST call: docs/api-v1.md has the read surface, docs/ws-protocol.md the
/// driving one), while `stop` is the REST call that waits for the terminal
/// state. Keeping them here means a screen never speaks the wire protocol.
class SessionActions {
  const SessionActions({required this.socket, required this.client});

  final PiUiSocket socket;
  final PiUiClient client;

  /// How long a send issued while reconnecting waits for the connection.
  static const reconnectBudget = Duration(seconds: 20);

  /// A plain prompt. pi answers `busy_streaming` while a run is in flight.
  Future<void> prompt(String sessionId, String text) => socket.command(
    sessionId: sessionId,
    op: 'session.prompt',
    payload: {'message': text},
    waitForConnection: reconnectBudget,
  );

  /// Steers the run in flight: delivered after the current tool calls.
  Future<void> steer(String sessionId, String text) => socket.command(
    sessionId: sessionId,
    op: 'session.steer',
    payload: {'message': text},
    waitForConnection: reconnectBudget,
  );

  /// Queues a follow-up: delivered when the agent settles.
  Future<void> followUp(String sessionId, String text) => socket.command(
    sessionId: sessionId,
    op: 'session.follow_up',
    payload: {'message': text},
    waitForConnection: reconnectBudget,
  );

  /// Drops the queued steering and follow-up messages.
  Future<void> clearQueue(String sessionId) =>
      socket.command(sessionId: sessionId, op: 'session.clear_queue');

  /// Aborts the run in flight.
  Future<void> abort(String sessionId) =>
      socket.command(sessionId: sessionId, op: 'session.abort');

  /// Renames the session (pi owns the name, the server follows).
  Future<void> rename(String sessionId, String name) => socket.command(
    sessionId: sessionId,
    op: 'session.rename',
    payload: {'name': name},
  );

  /// Stops the child and waits for it to be gone.
  Future<SessionModel> stop(String sessionId) => client.stopSession(sessionId);

  /// Asks the child for its own state (`get_state`), so the header can show the
  /// model, the thinking level and the queue depth the projection does not carry.
  Future<Map<String, dynamic>?> state(String sessionId) => socket.command(
    sessionId: sessionId,
    op: 'session.command.raw',
    payload: {'type': 'get_state'},
  );

  /// Asks the child for token and cost statistics (`get_session_stats`).
  Future<Map<String, dynamic>?> stats(String sessionId) => socket.command(
    sessionId: sessionId,
    op: 'session.command.raw',
    payload: {'type': 'get_session_stats'},
  );

  /// Asks pi which models are configured (`get_available_models`).
  ///
  /// It goes through `session.command.raw` on purpose: a new pi command must flow
  /// through the generic passthrough instead of a server-side special case
  /// (`AGENTS.md`, "pi is orchestrated, never forked").
  Future<List<ModelOption>> models(String sessionId) async {
    final data = await socket.command(
      sessionId: sessionId,
      op: 'session.command.raw',
      payload: {'type': 'get_available_models'},
    );
    return ModelOption.listFrom(data);
  }

  /// Switches the session onto one model (`set_model`).
  Future<void> setModel(String sessionId, ModelOption model) => socket.command(
    sessionId: sessionId,
    op: 'session.command.raw',
    payload: {
      'type': 'set_model',
      'provider': model.provider,
      'modelId': model.id,
    },
  );

  /// Sets the reasoning level (`set_thinking_level`).
  Future<void> setThinkingLevel(String sessionId, String level) =>
      socket.command(
        sessionId: sessionId,
        op: 'session.command.raw',
        payload: {'type': 'set_thinking_level', 'level': level},
      );

  /// Compacts the context now (`/compact`).
  Future<void> compact(String sessionId) => socket.command(
    sessionId: sessionId,
    op: 'session.command.raw',
    payload: {'type': 'compact'},
  );
}
