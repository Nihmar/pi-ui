import 'dart:async';

import '../models/chat_entry.dart';
import '../models/scenario.dart';
import '../models/session.dart';

/// The fake server of the mockup.
///
/// It exposes the same streams and commands the real client consumes — sessions,
/// the per-session timeline, the queue, the dialogs and the connection state —
/// and plays scripted [MockScenario]s through the same shapes the server emits.
/// No network, no child process, deterministic timing (scaled by [tick]).
class MockPiApi {
  MockPiApi({this.tick = const Duration(milliseconds: 40)});

  /// The base delay of a scenario step. Tests shrink it to run in milliseconds.
  final Duration tick;

  final _sessions = _Replay<List<SessionModel>>(const []);
  final _entries = <String, _Replay<List<ChatEntry>>>{};
  final _queues = <String, _Replay<List<QueueItem>>>{};
  final _dialogs = <String, _Replay<DialogRequest?>>{};
  final _connection = _Replay<MockConnection>(MockConnection.online);
  final _timers = <String, List<Timer>>{};

  var _sequence = 0;
  var _disposed = false;

  /// The sessions, newest first, with the current state of each.
  Stream<List<SessionModel>> get sessions => _sessions.stream;

  /// The connection state rendered by the global banner.
  Stream<MockConnection> get connection => _connection.stream;

  /// The timeline of one session.
  Stream<List<ChatEntry>> entriesOf(String sessionId) =>
      _replay<List<ChatEntry>>(_entries, sessionId, const <ChatEntry>[]).stream;

  /// The queued messages of one session.
  Stream<List<QueueItem>> queueOf(String sessionId) =>
      _replay<List<QueueItem>>(_queues, sessionId, const <QueueItem>[]).stream;

  /// The dialog waiting for an answer in that session, if any.
  Stream<DialogRequest?> dialogOf(String sessionId) =>
      _replay<DialogRequest?>(_dialogs, sessionId, null).stream;

  /// The current sessions, for callers that do not want a stream (tests).
  List<SessionModel> get currentSessions => List.unmodifiable(_sessions.value);

  /// The current timeline of one session.
  List<ChatEntry> currentEntries(String sessionId) =>
      List.unmodifiable(_entries[sessionId]?.value ?? const []);

  /// The current queued messages of one session.
  List<QueueItem> currentQueue(String sessionId) =>
      List.unmodifiable(_queues[sessionId]?.value ?? const []);

  /// The dialog currently waiting in that session, if any.
  DialogRequest? currentDialog(String sessionId) => _dialogs[sessionId]?.value;

  /// One session, live.
  SessionModel? session(String id) {
    for (final session in _sessions.value) {
      if (session.id == id) {
        return session;
      }
    }
    return null;
  }

  /// Fills the app with two sessions a reviewer can open immediately.
  void seed() {
    final now = DateTime.now();
    final alpha = _newId();
    final beta = _newId();
    _entries[alpha] = _Replay<List<ChatEntry>>([
      UserMessage(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(minutes: 12)),
        text: 'Read the repository and tell me what the server does.',
      ),
      AssistantMessage(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(minutes: 11)),
        thinking: 'I should start from the README and the docs directory.',
        text:
            '**pi-ui** wraps the `pi` coding agent.\n\n'
            'The Go server owns one `pi --mode rpc` child per session and exposes:\n\n'
            '- REST `/api/v1`\n'
            '- WebSocket `/ws/v1` (events, replay, dialogs)\n\n'
            '```bash\n./server/bin/pi-ui serve --addr 127.0.0.1:8787\n```\n\n'
            'The client never sees provider secrets.',
      ),
      ToolCallEntry(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(minutes: 10)),
        name: 'bash',
        title: 'List the repository root',
        command: 'ls -la',
        status: ToolStatus.success,
        output: 'AGENTS.md  README.md  bridge  docs  mockups  packages  schemas  server',
        duration: const Duration(milliseconds: 120),
      ),
      StatusEntry(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(minutes: 9)),
        text: 'Compacted context: 42 000 → 12 400 tokens',
      ),
    ]);
    _queues[alpha] = _Replay<List<QueueItem>>(const []);
    _dialogs[alpha] = _Replay<DialogRequest?>(null);

    _entries[beta] = _Replay<List<ChatEntry>>([
      UserMessage(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(hours: 2)),
        text: 'Add the retry to the sync queue.',
      ),
      AssistantMessage(
        id: 'e-${_next()}',
        at: now.subtract(const Duration(hours: 1, minutes: 58)),
        text: 'Done. The queue now retries when the network comes back.',
      ),
    ]);
    _queues[beta] = _Replay<List<QueueItem>>(const []);
    _dialogs[beta] = _Replay<DialogRequest?>(null);

    _sessions.add([
      SessionModel(
        id: alpha,
        cwd: '/home/user/Projects/pi-ui',
        name: 'pi-ui',
        status: SessionStatus.ready,
        createdAt: now.subtract(const Duration(minutes: 15)),
        pid: 4212,
        piSessionId: '01a0dccc-5d22-7411-99db-ae9b2df58d10',
        modelId: 'qwen-38-27b',
        provider: 'llama.cpp',
        thinkingLevel: 'medium',
        messageCount: 4,
        contextTokens: 12400,
        contextWindow: 128000,
        costUsd: 0.42,
        lastEventAt: now.subtract(const Duration(minutes: 9)),
      ),
      SessionModel(
        id: beta,
        cwd: '/home/user/Projects/Niman',
        name: 'Niman',
        status: SessionStatus.exited,
        createdAt: now.subtract(const Duration(hours: 2, minutes: 5)),
        pid: 3980,
        modelId: 'claude-sonnet-4-5',
        provider: 'anthropic',
        thinkingLevel: 'high',
        messageCount: 28,
        contextTokens: 51200,
        contextWindow: 200000,
        costUsd: 3.18,
        lastEventAt: now.subtract(const Duration(hours: 1, minutes: 58)),
        exitCode: 0,
      ),
    ]);
  }

  /// Starts a new session and settles it into `ready` after two ticks.
  SessionModel createSession({required String cwd, String? name}) {
    final now = DateTime.now();
    final id = _newId();
    final model = SessionModel(
      id: id,
      cwd: cwd,
      name: name,
      status: SessionStatus.spawning,
      createdAt: now,
      modelId: 'qwen-38-27b',
      provider: 'llama.cpp',
      thinkingLevel: 'medium',
      contextWindow: 128000,
    );
    _entries[id] = _Replay<List<ChatEntry>>(const []);
    _queues[id] = _Replay<List<QueueItem>>(const []);
    _dialogs[id] = _Replay<DialogRequest?>(null);
    _sessions.add([model, ..._sessions.value]);
    _after(id, tick * 2, () {
      _updateSession(
        id,
        (session) => session.copyWith(
          status: SessionStatus.ready,
          pid: 4300 + _sequence,
        ),
      );
      _append(
        id,
        StatusEntry(
          id: 'e-${_next()}',
          at: DateTime.now(),
          text: 'Session ready',
          kind: StatusKind.success,
        ),
      );
    });
    return model;
  }

  /// Forgets a session (the mockup keeps the JSONL, the list entry goes away).
  void removeSession(String id) {
    _cancel(id);
    _sessions.add([
      for (final session in _sessions.value)
        if (session.id != id) session,
    ]);
    _entries.remove(id)?.dispose();
    _queues.remove(id)?.dispose();
    _dialogs.remove(id)?.dispose();
  }

  /// Renames a session (the pi `rename` command and the projection follow).
  void renameSession(String id, String name) {
    _updateSession(id, (session) => session.copyWith(name: name));
    _append(
      id,
      StatusEntry(
        id: 'e-${_next()}',
        at: DateTime.now(),
        text: 'Renamed to "$name"',
      ),
    );
  }

  /// Stops a session the way `POST /sessions/{id}/stop` does.
  void stopSession(String id) {
    _cancel(id);
    _updateSession(
      id,
      (session) => session.copyWith(status: SessionStatus.stopping),
    );
    _append(
      id,
      StatusEntry(
        id: 'e-${_next()}',
        at: DateTime.now(),
        text: 'Closing the session…',
      ),
    );
    _after(id, tick * 3, () {
      _updateSession(
        id,
        (session) =>
            session.copyWith(status: SessionStatus.exited, exitCode: 0),
      );
      _append(
        id,
        StatusEntry(
          id: 'e-${_next()}',
          at: DateTime.now(),
          text: 'Session exited (0)',
          kind: StatusKind.success,
        ),
      );
    });
  }

  /// Sends a prompt: appends the user message and plays the streaming answer.
  void sendPrompt(String id, String text, {bool queued = false}) {
    _append(
      id,
      UserMessage(
        id: 'e-${_next()}',
        at: DateTime.now(),
        text: text,
        queued: queued,
      ),
    );
    _updateSession(
      id,
      (session) => session.copyWith(
        status: SessionStatus.streaming,
        messageCount: session.messageCount + 1,
      ),
    );
    _playStreamingAnswer(id);
  }

  /// Queues a steering message (`session.steer`).
  void steer(String id, String text) {
    _enqueue(id, QueueItem(id: 'q-${_next()}', kind: 'steer', text: text));
    _updateSession(
      id,
      (session) =>
          session.copyWith(pendingMessages: session.pendingMessages + 1),
    );
  }

  /// Queues a follow-up (`session.follow_up`).
  void followUp(String id, String text) {
    _enqueue(id, QueueItem(id: 'q-${_next()}', kind: 'follow_up', text: text));
    _updateSession(
      id,
      (session) =>
          session.copyWith(pendingMessages: session.pendingMessages + 1),
    );
  }

  /// Drops one queued message.
  void cancelQueued(String id, String itemId) {
    final queue = _queues[id];
    if (queue == null) {
      return;
    }
    queue.add([
      for (final item in queue.value)
        if (item.id != itemId) item,
    ]);
    _updateSession(
      id,
      (session) => session.copyWith(
        pendingMessages: session.pendingMessages > 0
            ? session.pendingMessages - 1
            : 0,
      ),
    );
  }

  /// Aborts the run: the stream stops, the session goes back to ready.
  void abort(String id) {
    _cancel(id);
    _finishStreaming(id);
    _append(
      id,
      StatusEntry(id: 'e-${_next()}', at: DateTime.now(), text: 'Run aborted'),
    );
  }

  /// Answers the pending dialog; the child gets exactly one answer.
  void answerDialog(
    String sessionId, {
    String? value,
    bool? confirmed,
    bool cancelled = false,
  }) {
    final dialog = _dialogs[sessionId]?.value;
    if (dialog == null) {
      return;
    }
    _dialogs[sessionId]!.add(null);
    final answer = cancelled
        ? 'cancelled'
        : confirmed != null
        ? (confirmed ? 'approved' : 'declined')
        : '"$value"';
    _append(
      sessionId,
      StatusEntry(
        id: 'e-${_next()}',
        at: DateTime.now(),
        text: 'Dialog "${dialog.title}" $answer',
        kind: confirmed == false || cancelled
            ? StatusKind.warning
            : StatusKind.success,
      ),
    );
  }

  /// Flips the connection state; `reconnecting` resolves to `online` after a few
  /// ticks and an `offline` one flushes its queue when it comes back.
  void setConnection(MockConnection state) {
    _connection.add(state);
    if (state == MockConnection.reconnecting) {
      Timer(tick * 6, () {
        if (_disposed) {
          return;
        }
        _connection.add(MockConnection.online);
        _flushQueue(_offlineSession ?? _lastSessionId ?? '');
      });
    }
    if (state == MockConnection.online) {
      _flushQueue(_offlineSession ?? _lastSessionId ?? '');
    }
  }

  /// Plays one scripted scenario on a session.
  void runScenario(MockScenario scenario, String sessionId) {
    _cancel(sessionId);
    switch (scenario) {
      case MockScenario.streaming:
        sendPrompt(
          sessionId,
          'Walk me through the reconnect path of the server.',
        );
      case MockScenario.toolCall:
        _playToolCalls(sessionId);
      case MockScenario.extensionDialog:
        _openDialog(sessionId, timeout: const Duration(seconds: 20));
      case MockScenario.dialogTimeout:
        _openDialog(sessionId, timeout: tick * 4);
      case MockScenario.providerError:
        _append(
          sessionId,
          ErrorEntry(
            id: 'e-${_next()}',
            at: DateTime.now(),
            title: 'Provider quota exhausted',
            message: 'The provider rejected the request: quota exceeded. Retry after the window resets.',
            code: 'model_provider_error',
            actionLabel: 'Retry',
          ),
        );
        _updateSession(
          sessionId,
          (session) => session.copyWith(status: SessionStatus.ready),
        );
      case MockScenario.sessionCrash:
        _updateSession(
          sessionId,
          (session) =>
              session.copyWith(status: SessionStatus.crashed, exitCode: 9),
        );
        _append(
          sessionId,
          ErrorEntry(
            id: 'e-${_next()}',
            at: DateTime.now(),
            title: 'Session crashed',
            message: 'The child exited with code 9. The session stays listed; create a new one to continue.',
            code: 'session_exited',
            actionLabel: 'Restart session',
          ),
        );
      case MockScenario.offlineQueue:
        _connection.add(MockConnection.offline);
        _offlineSession = sessionId;
        steer(sessionId, 'Actually, run the tests first.');
        followUp(sessionId, 'Then update the changelog.');
      case MockScenario.reconnection:
        setConnection(MockConnection.reconnecting);
        _append(
          sessionId,
          StatusEntry(
            id: 'e-${_next()}',
            at: DateTime.now(),
            text: 'Replaying 12 missed events…',
            spinner: true,
          ),
        );
        _after(sessionId, tick * 6, () {
          _append(
            sessionId,
            StatusEntry(
              id: 'e-${_next()}',
              at: DateTime.now(),
              text: 'Caught up: 12 events replayed',
              kind: StatusKind.success,
            ),
          );
        });
      case MockScenario.wrapUp:
        _updateSession(
          sessionId,
          (session) => session.copyWith(status: SessionStatus.stopping),
        );
        _append(
          sessionId,
          StatusEntry(
            id: 'e-${_next()}',
            at: DateTime.now(),
            text: 'Closing… the agent is wrapping up',
            spinner: true,
          ),
        );
        _after(sessionId, tick * 3, () {
          _append(
            sessionId,
            StatusEntry(
              id: 'e-${_next()}',
              at: DateTime.now(),
              text: 'Handoff saved to ~/.pi/handoff/2026-09-26-pi-ui.md',
              kind: StatusKind.success,
            ),
          );
        });
        _after(sessionId, tick * 5, () {
          _updateSession(
            sessionId,
            (session) =>
                session.copyWith(status: SessionStatus.exited, exitCode: 0),
          );
        });
    }
  }

  /// Whether [scenario] needs a live session, for the recorder's enable state.
  static bool needsLiveSession(MockScenario scenario) =>
      scenario != MockScenario.providerError &&
      scenario != MockScenario.sessionCrash;

  // ---------------------------------------------------------------- scenarios

  void _playStreamingAnswer(String id) {
    const answer =
        'The reconnect path is the durable-replay one:\n\n'
        '1. The client reconnects and sends `subscribe` with `since.entryId`.\n'
        '2. The hub asks the session for `get_entries` and re-emits them as '
        '`pi.entry_appended`.\n'
        '3. Live events published during the replay are buffered and flushed '
        'afterwards, deduplicated by entry id.\n\n'
        '```json\n{"type":"subscribe","sessionId":"s_…","since":{"entryId":"e-12"}}\n```\n\n'
        'So a client never renders a hole, and never an event twice.';
    final entry = AssistantMessage(
      id: 'e-${_next()}',
      at: DateTime.now(),
      text: '',
      thinking:
          'The reconnect path is the durable replay; explain it in order.',
      streaming: true,
    );
    _append(id, entry);

    final chunks = _chunks(answer, 6);
    for (var index = 0; index < chunks.length; index++) {
      final soFar = chunks.take(index + 1).join();
      _after(id, tick * (index + 1), () {
        _replaceEntry(
          id,
          entry.id,
          entry.copyWith(text: soFar, streaming: index < chunks.length - 1),
        );
      });
    }
    _after(id, tick * (chunks.length + 1), () {
      _finishStreaming(id);
    });
  }

  void _playToolCalls(String id) {
    final bash = ToolCallEntry(
      id: 'e-${_next()}',
      at: DateTime.now(),
      name: 'bash',
      title: 'Run the server tests',
      command: 'go test -race ./...',
      status: ToolStatus.running,
    );
    _append(id, bash);
    _updateSession(
      id,
      (session) => session.copyWith(status: SessionStatus.streaming),
    );

    final started = DateTime.now();
    _after(id, tick * 3, () {
      _replaceEntry(
        id,
        bash.id,
        bash.copyWith(
          status: ToolStatus.success,
          duration: DateTime.now().difference(started),
          output:
              'ok  github.com/Nihmar/pi-ui/server/internal/rpc\n'
              'ok  github.com/Nihmar/pi-ui/server/internal/ws\n'
              'ok  github.com/Nihmar/pi-ui/server/test/adversarial',
        ),
      );
      _append(
        id,
        AssistantMessage(
          id: 'e-${_next()}',
          at: DateTime.now(),
          text: 'Tests are green. Now the diff:',
        ),
      );
    });

    final edit = ToolCallEntry(
      id: 'e-${_next()}',
      at: DateTime.now(),
      name: 'edit',
      title: 'Edit internal/ws/replay.go',
      command: 'edit',
      status: ToolStatus.running,
    );
    _after(id, tick * 4, () => _append(id, edit));
    _after(id, tick * 6, () {
      _replaceEntry(
        id,
        edit.id,
        edit.copyWith(
          status: ToolStatus.success,
          diff: const DiffPreview(
            path: 'server/internal/ws/replay.go',
            added: 2,
            removed: 1,
            lines: [
              DiffLine(
                DiffLineKind.context,
                ' func (s *subscription) flushLocked(h *hub) {',
              ),
              DiffLine(
                DiffLineKind.removed,
                '    sort.SliceStable(pending, bySeq)',
              ),
              DiffLine(
                DiffLineKind.added,
                '    // Dedup by entry id on the durable path.',
              ),
              DiffLine(DiffLineKind.added, '    for _, ev := range pending {'),
              DiffLine(DiffLineKind.context, ' }'),
            ],
          ),
          duration: const Duration(milliseconds: 240),
        ),
      );
      _updateSession(
        id,
        (session) => session.copyWith(
          status: SessionStatus.ready,
          contextTokens: session.contextTokens + 2400,
        ),
      );
      _append(
        id,
        StatusEntry(
          id: 'e-${_next()}',
          at: DateTime.now(),
          text: 'Context 14 800 / 128 000',
          kind: StatusKind.info,
        ),
      );
    });
  }

  void _openDialog(String id, {required Duration timeout}) {
    final dialog = DialogRequest(
      id: 'dlg-${_next()}',
      sessionId: id,
      method: DialogMethod.confirm,
      title: 'Run bash command?',
      message: 'rm -rf build/ && flutter build apk --debug',
      at: DateTime.now(),
      expiresAt: DateTime.now().add(timeout),
    );
    _dialogs[id]!.add(dialog);
    final expiresAt = dialog.expiresAt;
    _after(id, timeout, () {
      if (_dialogs[id]?.value?.id != dialog.id) {
        return;
      }
      _dialogs[id]!.add(null);
      _append(
        id,
        StatusEntry(
          id: 'e-${_next()}',
          at: DateTime.now(),
          text:
              'Dialog "${dialog.title}" timed out after ${expiresAt.difference(dialog.at).inSeconds}s',
          kind: StatusKind.warning,
        ),
      );
    });
  }

  void _flushQueue(String sessionId) {
    final queue = _queues[sessionId];
    if (queue == null || queue.value.isEmpty) {
      return;
    }
    final items = queue.value;
    queue.add(const []);
    _updateSession(
      sessionId,
      (session) => session.copyWith(pendingMessages: 0),
    );
    for (final item in items) {
      sendPrompt(sessionId, item.text);
    }
  }

  void _finishStreaming(String id) {
    final entries = _entries[id]?.value;
    if (entries == null) {
      return;
    }
    for (final entry in entries) {
      if (entry is AssistantMessage && entry.streaming) {
        _replaceEntry(id, entry.id, entry.copyWith(streaming: false));
      }
    }
    _updateSession(
      id,
      (session) => session.copyWith(
        status: SessionStatus.ready,
        costUsd: session.costUsd + 0.03,
        contextTokens: session.contextTokens + 1800,
      ),
    );
  }

  // ------------------------------------------------------------------ plumbing

  String? _offlineSession;

  String? _lastSessionId;

  String _newId() {
    final suffix = _next().toRadixString(16).padLeft(16, '0');
    return 's_${suffix.substring(suffix.length - 16)}';
  }

  int _next() => ++_sequence;

  /// Schedules [action] after [delay] from now, cancellable with the session.
  void _after(String sessionId, Duration delay, void Function() action) {
    final list = _timers.putIfAbsent(sessionId, () => <Timer>[]);
    list.add(
      Timer(delay, () {
        if (_disposed) {
          return;
        }
        action();
      }),
    );
  }

  void _cancel(String sessionId) {
    final list = _timers.remove(sessionId);
    if (list == null) {
      return;
    }
    for (final timer in list) {
      timer.cancel();
    }
  }

  void _append(String sessionId, ChatEntry entry) {
    final replay = _replay<List<ChatEntry>>(
      _entries,
      sessionId,
      const <ChatEntry>[],
    );
    replay.add([...replay.value, entry]);
    if (entry is UserMessage) {
      _lastSessionId = sessionId;
    }
  }

  void _replaceEntry(String sessionId, String entryId, ChatEntry replacement) {
    final replay = _replay<List<ChatEntry>>(
      _entries,
      sessionId,
      const <ChatEntry>[],
    );
    replay.add([
      for (final entry in replay.value)
        if (entry.id == entryId) replacement else entry,
    ]);
  }

  void _enqueue(String sessionId, QueueItem item) {
    final replay = _replay<List<QueueItem>>(
      _queues,
      sessionId,
      const <QueueItem>[],
    );
    replay.add([...replay.value, item]);
  }

  void _updateSession(
    String id,
    SessionModel Function(SessionModel session) change,
  ) {
    _sessions.add([
      for (final session in _sessions.value)
        if (session.id == id) change(session) else session,
    ]);
  }

  _Replay<T> _replay<T>(Map<String, _Replay<T>> map, String key, T initial) {
    return map.putIfAbsent(key, () => _Replay<T>(initial));
  }

  /// Releases every timer and stream.
  void dispose() {
    _disposed = true;
    for (final list in _timers.values) {
      for (final timer in list) {
        timer.cancel();
      }
    }
    _timers.clear();
    _sessions.dispose();
    _connection.dispose();
    for (final replay in _entries.values) {
      replay.dispose();
    }
    for (final replay in _queues.values) {
      replay.dispose();
    }
    for (final replay in _dialogs.values) {
      replay.dispose();
    }
  }

  static List<String> _chunks(String text, int size) {
    final words = text.split(' ');
    final chunks = <String>[];
    for (var index = 0; index < words.length; index += size) {
      final end = (index + size).clamp(0, words.length);
      chunks.add(
        '${words.sublist(index, end).join(' ')}${end == words.length ? '' : ' '}',
      );
    }
    return chunks;
  }
}

/// A broadcast stream that replays its current value to **every** new listener, so
/// a screen that subscribes late still renders the state it missed.
///
/// `onListen` is not enough here: it fires when the first listener arrives, and a
/// second provider watching the same stream would then wait for the next change.
/// `Stream.multi` gives each listener the current value before it joins the
/// broadcast.
class _Replay<T> {
  _Replay(this._value);

  final StreamController<T> _controller = StreamController<T>.broadcast();
  T _value;

  T get value => _value;

  Stream<T> get stream => Stream<T>.multi((controller) {
    controller.add(_value);
    final subscription = _controller.stream.listen(
      controller.add,
      onError: controller.addError,
      onDone: () {
        if (!controller.isClosed) {
          controller.close();
        }
      },
    );
    controller.onCancel = subscription.cancel;
  });

  void add(T value) {
    _value = value;
    if (!_controller.isClosed) {
      _controller.add(value);
    }
  }

  void dispose() => _controller.close();
}
