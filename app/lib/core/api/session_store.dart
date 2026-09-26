import '../models/session.dart';
import 'dto.dart';
import 'frames.dart';
import 'json.dart';

/// Applies one server frame to the session list.
///
/// The REST list stays the seed: every lifecycle event carries the server's own
/// projection (`sessions.Info`), so a frame patches the list instead of making
/// the client fetch it again. Returns the same list when nothing changed, which
/// is what lets the caller skip an emission (`identical`).
List<SessionModel> applySessionFrame(
  List<SessionModel> sessions,
  WsFrame frame,
) {
  if (frame is! WsEvent) {
    return sessions;
  }
  final sessionId = frame.sessionId;
  if (sessionId == null) {
    return sessions;
  }
  final payload = frame.payload;
  switch (frame.type) {
    case 'server.spawned':
    case 'server.ready':
    case 'server.status':
    case 'server.stopping':
      if (optStr(payload['id']) == null) {
        return sessions;
      }
      return _upsert(sessions, sessionFromJson(payload));
    case 'server.exited':
    case 'server.crashed':
      return _patch(
        sessions,
        sessionId,
        (session) => session.copyWith(
          status: frame.type == 'server.crashed'
              ? SessionStatus.crashed
              : SessionStatus.exited,
          exitCode: intOf(payload['exitCode'], fallback: -1),
          lastEventAt: frame.at,
        ),
      );
    case 'pi.session_info_changed':
      final name = optStr(payload['name']);
      return _patch(
        sessions,
        sessionId,
        (session) => session.copyWith(name: name ?? session.name),
      );
    case 'pi.thinking_level_changed':
      return _patch(
        sessions,
        sessionId,
        (session) => session.copyWith(thinkingLevel: optStr(payload['level'])),
      );
    case 'pi.queue_update':
      final pending =
          asList(payload['steering']).length +
          asList(payload['followUp']).length;
      return _patch(
        sessions,
        sessionId,
        (session) => session.copyWith(pendingMessages: pending),
      );
    case 'pi.agent_settled' || 'pi.agent_end':
      return _patch(sessions, sessionId, (session) {
        if (session.status != SessionStatus.streaming) {
          return session;
        }
        return session.copyWith(
          status: SessionStatus.ready,
          lastEventAt: frame.at,
        );
      });
    case 'pi.message_update':
      return _patch(
        sessions,
        sessionId,
        (session) => session.status == SessionStatus.ready
            ? session.copyWith(
                status: SessionStatus.streaming,
                lastEventAt: frame.at,
              )
            : session.copyWith(lastEventAt: frame.at),
      );
    case 'pi.entry_appended':
      final entry = asMap(payload['entry']);
      final counting =
          str(entry?['type']) == 'message' &&
          const {
            'user',
            'assistant',
            'toolResult',
          }.contains(str(asMap(entry?['message'])?['role']));
      return _patch(
        sessions,
        sessionId,
        (session) => counting
            ? session.copyWith(
                messageCount: session.messageCount + 1,
                lastEventAt: frame.at,
              )
            : session.copyWith(lastEventAt: frame.at),
      );
    default:
      return sessions;
  }
}

/// Merges a session from a lifecycle payload, keeping the list's order.
List<SessionModel> _upsert(List<SessionModel> sessions, SessionModel incoming) {
  final index = sessions.indexWhere((session) => session.id == incoming.id);
  if (index < 0) {
    return [...sessions, incoming];
  }
  if (_sameSession(sessions[index], incoming)) {
    return sessions;
  }
  final updated = [...sessions];
  updated[index] = incoming;
  return updated;
}

/// Patches one session; returns the same list when the patch changes nothing or
/// the session is unknown (a frame for a session the REST list never returned).
List<SessionModel> _patch(
  List<SessionModel> sessions,
  String id,
  SessionModel Function(SessionModel session) patch,
) {
  final index = sessions.indexWhere((session) => session.id == id);
  if (index < 0) {
    return sessions;
  }
  final patched = patch(sessions[index]);
  if (_sameSession(sessions[index], patched)) {
    return sessions;
  }
  final updated = [...sessions];
  updated[index] = patched;
  return updated;
}

bool _sameSession(SessionModel a, SessionModel b) =>
    a.id == b.id &&
    a.cwd == b.cwd &&
    a.name == b.name &&
    a.status == b.status &&
    a.pid == b.pid &&
    a.piSessionId == b.piSessionId &&
    a.modelId == b.modelId &&
    a.provider == b.provider &&
    a.thinkingLevel == b.thinkingLevel &&
    a.messageCount == b.messageCount &&
    a.contextTokens == b.contextTokens &&
    a.contextWindow == b.contextWindow &&
    a.costUsd == b.costUsd &&
    a.pendingMessages == b.pendingMessages &&
    a.lastEventAt == b.lastEventAt &&
    a.exitCode == b.exitCode;
