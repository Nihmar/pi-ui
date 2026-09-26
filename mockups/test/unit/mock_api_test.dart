import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui_mockups/core/data/mock_api.dart';
import 'package:piui_mockups/core/models/chat_entry.dart';
import 'package:piui_mockups/core/models/scenario.dart';
import 'package:piui_mockups/core/models/session.dart';

void main() {
  late MockPiApi api;
  late String sessionId;

  setUp(() {
    api = MockPiApi(tick: const Duration(milliseconds: 10))..seed();
    sessionId = api.currentSessions.first.id;
  });

  tearDown(() => api.dispose());

  test('seed gives two sessions with history', () {
    expect(api.currentSessions, hasLength(2));
    expect(api.currentEntries(sessionId), isNotEmpty);
    expect(api.currentSessions.first.status, SessionStatus.ready);
    expect(api.currentSessions.last.status, SessionStatus.exited);
  });

  test('createSession settles from spawning to ready', () {
    fakeAsync((async) {
      final created = api.createSession(cwd: '/tmp/project', name: 'project');
      expect(api.session(created.id)!.status, SessionStatus.spawning);

      async.elapse(const Duration(milliseconds: 50));

      expect(api.session(created.id)!.status, SessionStatus.ready);
      expect(api.currentSessions.first.id, created.id);
    });
  });

  test('a prompt streams an answer and finishes the run', () {
    fakeAsync((async) {
      api.sendPrompt(sessionId, 'hello');
      expect(api.session(sessionId)!.status, SessionStatus.streaming);

      final streaming = api
          .currentEntries(sessionId)
          .whereType<AssistantMessage>()
          .last;
      expect(streaming.streaming, isTrue);
      expect(streaming.thinking, isNotNull);

      async.elapse(const Duration(seconds: 5));

      final finished = api
          .currentEntries(sessionId)
          .whereType<AssistantMessage>()
          .last;
      expect(finished.streaming, isFalse);
      expect(finished.text, contains('durable-replay'));
      expect(api.session(sessionId)!.status, SessionStatus.ready);
    });
  });

  test('abort stops the stream and returns the session to ready', () {
    fakeAsync((async) {
      api.sendPrompt(sessionId, 'hello');
      async.elapse(const Duration(milliseconds: 30));
      api.abort(sessionId);
      async.elapse(const Duration(seconds: 2));

      final assistant = api
          .currentEntries(sessionId)
          .whereType<AssistantMessage>()
          .last;
      expect(assistant.streaming, isFalse);
      expect(api.session(sessionId)!.status, SessionStatus.ready);
      expect(
        api
            .currentEntries(sessionId)
            .whereType<StatusEntry>()
            .any((entry) => entry.text == 'Run aborted'),
        isTrue,
      );
    });
  });

  test('the tool-call scenario produces output and a diff', () {
    fakeAsync((async) {
      api.runScenario(MockScenario.toolCall, sessionId);
      async.elapse(const Duration(seconds: 2));

      final calls = api
          .currentEntries(sessionId)
          .whereType<ToolCallEntry>()
          .toList();
      final created = calls.sublist(calls.length - 2);
      expect(created, hasLength(2));
      expect(created.first.name, 'bash');
      expect(created.first.status, ToolStatus.success);
      expect(created.first.output, contains('ok '));
      expect(created.last.diff, isNotNull);
      expect(created.last.diff!.added, 2);
      expect(api.session(sessionId)!.status, SessionStatus.ready);
    });
  });

  test('a dialog is answered once and leaves a status line', () {
    fakeAsync((async) {
      api.runScenario(MockScenario.extensionDialog, sessionId);
      final dialog = api.currentDialog(sessionId);
      expect(dialog, isNotNull);
      expect(dialog!.method, DialogMethod.confirm);
      expect(dialog.remainingSeconds(DateTime.now()), greaterThan(0));

      api.answerDialog(sessionId, confirmed: true);
      expect(api.currentDialog(sessionId), isNull);
      expect(
        api.currentEntries(sessionId).whereType<StatusEntry>().last.text,
        contains('approved'),
      );
    });
  });

  test('an unanswered dialog times out with a warning', () {
    fakeAsync((async) {
      api.runScenario(MockScenario.dialogTimeout, sessionId);
      expect(api.currentDialog(sessionId), isNotNull);

      async.elapse(const Duration(seconds: 1));

      expect(api.currentDialog(sessionId), isNull);
      final last = api.currentEntries(sessionId).whereType<StatusEntry>().last;
      expect(last.kind, StatusKind.warning);
      expect(last.text, contains('timed out'));
    });
  });

  test('the provider error carries its taxonomy code', () {
    api.runScenario(MockScenario.providerError, sessionId);

    final error = api.currentEntries(sessionId).whereType<ErrorEntry>().single;
    expect(error.code, 'model_provider_error');
    expect(error.actionLabel, isNotNull);
  });

  test('the crash scenario marks the session crashed with its exit code', () {
    api.runScenario(MockScenario.sessionCrash, sessionId);

    final session = api.session(sessionId)!;
    expect(session.status, SessionStatus.crashed);
    expect(session.exitCode, 9);
    expect(
      api.currentEntries(sessionId).whereType<ErrorEntry>().single.title,
      contains('crashed'),
    );
  });

  test('offline messages queue and flush when the connection returns', () {
    fakeAsync((async) {
      final before = api
          .currentEntries(sessionId)
          .whereType<UserMessage>()
          .length;
      api.runScenario(MockScenario.offlineQueue, sessionId);
      expect(api.currentQueue(sessionId), hasLength(2));
      expect(api.session(sessionId)!.pendingMessages, 2);

      api.setConnection(MockConnection.online);
      expect(api.currentQueue(sessionId), isEmpty);
      expect(api.session(sessionId)!.pendingMessages, 0);
      expect(
        api.currentEntries(sessionId).whereType<UserMessage>(),
        hasLength(before + 2),
      );

      async.elapse(const Duration(seconds: 5));
    });
  });

  test('the wrap-up scenario ends the session with a handoff', () {
    fakeAsync((async) {
      api.runScenario(MockScenario.wrapUp, sessionId);
      expect(api.session(sessionId)!.status, SessionStatus.stopping);

      async.elapse(const Duration(seconds: 1));

      expect(api.session(sessionId)!.status, SessionStatus.exited);
      expect(
        api
            .currentEntries(sessionId)
            .whereType<StatusEntry>()
            .any((entry) => entry.text.contains('Handoff saved')),
        isTrue,
      );
    });
  });
}
