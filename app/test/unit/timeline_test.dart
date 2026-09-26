import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/frames.dart';
import 'package:piui/core/api/timeline.dart';
import 'package:piui/core/models/chat_entry.dart';

/// Builds one event frame the way the server does.
WsEvent event(String type, Map<String, dynamic> payload, {int seq = 1}) =>
    parseFrame(
      jsonEncode({
        'type': type,
        'sessionId': 's_1',
        'seq': seq,
        'ts': '2026-09-26T12:00:00.000Z',
        'payload': payload,
      }),
    ) as WsEvent;

/// One `entry_appended` carrying [entry].
WsEvent entryEvent(
  Map<String, dynamic> entry, {
  int seq = 1,
  String type = 'pi.entry_appended',
}) => event(type, {'entry': entry}, seq: seq);

/// One assistant content block.
Map<String, dynamic> text(String value) => {'type': 'text', 'text': value};

void main() {
  group('entries', () {
    test('a user message keeps its id', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e1',
          'type': 'message',
          'timestamp': '2026-09-26T12:00:00.000Z',
          'message': {'role': 'user', 'content': 'hello'},
        }),
      );
      expect(state.entries, hasLength(1));
      final message = state.entries.single as UserMessage;
      expect(message.id, 'e1');
      expect(message.text, 'hello');
    });

    test('an assistant entry keeps the text, the thinking and the calls', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e2',
          'type': 'message',
          'message': {
            'role': 'assistant',
            'content': [
              {'type': 'thinking', 'thinking': 'let me look'},
              text('reading the file'),
              {
                'type': 'toolCall',
                'id': 'call-1',
                'name': 'read',
                'arguments': {'path': '/tmp/a.txt'},
              },
            ],
          },
        }),
      );
      expect(state.entries, hasLength(2));
      final message = state.entries.first as AssistantMessage;
      expect(message.text, 'reading the file');
      expect(message.thinking, 'let me look');
      expect(message.streaming, isFalse);
      final call = state.entries.last as ToolCallEntry;
      expect(call.id, 'tool:call-1');
      expect(call.name, 'read');
      expect(call.title, '/tmp/a.txt');
      expect(call.status, ToolStatus.pending);
      expect(state.streaming, isFalse);
    });

    test('a tool result fills the card of its call', () {
      var state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e2',
          'type': 'message',
          'message': {
            'role': 'assistant',
            'content': [
              {
                'type': 'toolCall',
                'id': 'call-1',
                'name': 'bash',
                'arguments': {'command': 'ls'},
              },
            ],
          },
        }),
      );
      state = applyFrame(
        state,
        entryEvent({
          'id': 'e3',
          'type': 'message',
          'message': {
            'role': 'toolResult',
            'toolCallId': 'call-1',
            'toolName': 'bash',
            'isError': false,
            'content': [text('a.txt\nb.txt')],
          },
        }, seq: 2),
      );
      expect(state.entries, hasLength(1));
      final card = state.entries.single as ToolCallEntry;
      expect(card.status, ToolStatus.success);
      expect(card.output, 'a.txt\nb.txt');
      expect(card.command, 'ls');
    });

    test('a failed tool result marks the card as failed', () {
      var state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e2',
          'type': 'message',
          'message': {
            'role': 'assistant',
            'content': [
              {
                'type': 'toolCall',
                'id': 'c',
                'name': 'bash',
                'arguments': {'command': 'false'},
              },
            ],
          },
        }),
      );
      state = applyFrame(
        state,
        entryEvent({
          'id': 'e3',
          'type': 'message',
          'message': {
            'role': 'toolResult',
            'toolCallId': 'c',
            'isError': true,
            'content': [text('boom')],
          },
        }, seq: 2),
      );
      expect((state.entries.single as ToolCallEntry).status, ToolStatus.error);
    });

    test('a tool result without a card still shows up', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e3',
          'type': 'message',
          'message': {
            'role': 'toolResult',
            'toolCallId': 'orphan',
            'toolName': 'grep',
            'isError': false,
            'content': [text('hit')],
          },
        }),
      );
      final card = state.entries.single as ToolCallEntry;
      expect(card.id, 'tool:orphan');
      expect(card.name, 'grep');
      expect(card.output, 'hit');
    });

    test('a bash execution is a card with its exit code', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e4',
          'type': 'message',
          'message': {
            'role': 'bashExecution',
            'command': 'make test',
            'output': 'ok',
            'exitCode': 1,
            'truncated': true,
            'fullOutputPath': '/tmp/full.log',
          },
        }),
      );
      final card = state.entries.single as ToolCallEntry;
      expect(card.name, 'bash');
      expect(card.status, ToolStatus.error);
      expect(card.fullOutputPath, '/tmp/full.log');
    });

    test('system and empty custom entries stay out of the timeline', () {
      var state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e5',
          'type': 'message',
          'message': {'role': 'system', 'content': 'the prompt'},
        }),
      );
      expect(state.entries, isEmpty);
      state = applyFrame(
        state,
        entryEvent({
          'id': 'e6',
          'type': 'custom',
          'customType': 'goal',
          'content': '',
        }, seq: 2),
      );
      expect(state.entries, isEmpty);
    });

    test('a custom entry with text becomes a status line', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({
          'id': 'e6',
          'type': 'custom',
          'customType': 'goal',
          'content': 'ship it',
        }),
      );
      final line = state.entries.single as StatusEntry;
      expect(line.text, 'goal: ship it');
    });

    test('a compaction entry becomes a status line', () {
      final state = applyFrame(
        const ChatState(),
        entryEvent({'id': 'e7', 'type': 'compaction', 'tokensBefore': 120000}),
      );
      expect((state.entries.single as StatusEntry).text, contains('120000'));
    });
  });

  group('streaming', () {
    WsEvent update(Map<String, dynamic> assistantEvent, {int seq = 1}) => event(
      'pi.message_update',
      {'assistantMessageEvent': assistantEvent},
      seq: seq,
    );

    test('deltas build one message that the final entry replaces', () {
      var state = applyFrame(const ChatState(), update({'type': 'start'}));
      expect(state.streaming, isTrue);
      state = applyFrame(
        state,
        update({'type': 'text_delta', 'delta': 'Hel'}, seq: 2),
      );
      state = applyFrame(
        state,
        update({'type': 'text_delta', 'delta': 'lo'}, seq: 3),
      );
      expect((state.entries.single as AssistantMessage).text, 'Hello');
      expect(state.entries.single.id, 'assistant:streaming');

      state = applyFrame(
        state,
        entryEvent({
          'id': 'e9',
          'type': 'message',
          'message': {
            'role': 'assistant',
            'content': [text('Hello!')],
          },
        }, seq: 4),
      );
      expect(state.entries, hasLength(1));
      expect((state.entries.single as AssistantMessage).id, 'e9');
      expect((state.entries.single as AssistantMessage).text, 'Hello!');
      expect(state.streaming, isFalse);
    });

    test('thinking deltas go to the thinking block', () {
      var state = applyFrame(const ChatState(), update({'type': 'start'}));
      state = applyFrame(
        state,
        update({'type': 'thinking_delta', 'delta': 'hmm'}, seq: 2),
      );
      expect((state.entries.single as AssistantMessage).thinking, 'hmm');
    });

    test('a tool call start adds a running card the entry reuses', () {
      var state = applyFrame(
        const ChatState(),
        update({'type': 'toolcall_start', 'id': 'call-9', 'toolName': 'bash'}),
      );
      expect(
        (state.entries.single as ToolCallEntry).status,
        ToolStatus.running,
      );
      state = applyFrame(
        state,
        update({
          'type': 'toolcall_end',
          'toolCall': {
            'type': 'toolCall',
            'id': 'call-9',
            'name': 'bash',
            'arguments': {'command': 'ls -la'},
          },
        }, seq: 2),
      );
      expect((state.entries.single as ToolCallEntry).title, 'ls -la');

      state = applyFrame(
        state,
        entryEvent({
          'id': 'e10',
          'type': 'message',
          'message': {
            'role': 'assistant',
            'content': [
              {
                'type': 'toolCall',
                'id': 'call-9',
                'name': 'bash',
                'arguments': {'command': 'ls -la'},
              },
            ],
          },
        }, seq: 3),
      );
      // The card from the deltas is kept, not duplicated.
      expect(state.entries.whereType<ToolCallEntry>(), hasLength(1));
    });

    test('agent_settled stops the spinner but keeps the text', () {
      var state = applyFrame(const ChatState(), update({'type': 'start'}));
      state = applyFrame(
        state,
        update({'type': 'text_delta', 'delta': 'partial'}, seq: 2),
      );
      state = applyFrame(state, event('pi.agent_settled', const {}, seq: 3));
      expect(state.streaming, isFalse);
      expect((state.entries.single as AssistantMessage).streaming, isFalse);
      expect((state.entries.single as AssistantMessage).text, 'partial');
    });

    test('an error update appends an error entry', () {
      var state = applyFrame(const ChatState(), update({'type': 'start'}));
      state = applyFrame(
        state,
        update({
          'type': 'error',
          'error': {'errorMessage': 'provider refused'},
        }, seq: 2),
      );
      expect(state.streaming, isFalse);
      final error = state.entries.whereType<ErrorEntry>().single;
      expect(error.message, 'provider refused');
    });
  });

  group('queue and dialogs', () {
    test('queue_update replaces the queue', () {
      final state = applyFrame(
        const ChatState(),
        event('pi.queue_update', {
          'steering': ['first'],
          'followUp': ['later'],
        }),
      );
      expect(state.queue.map((item) => item.text), ['first', 'later']);
      expect(state.queue.map((item) => item.kind), ['steer', 'follow_up']);
    });

    test('a request opens a dialog and its timeout closes it', () {
      var state = applyFrame(
        const ChatState(),
        parseFrame(
          jsonEncode({
            'type': 'request',
            'id': 'u1',
            'sessionId': 's_1',
            'method': 'confirm',
            'title': 'Bash',
            'timeoutMs': 1000,
          }),
        ),
      );
      expect(state.dialog?.id, 'u1');

      state = applyFrame(
        state,
        event('server.dialog.timeout', {'requestId': 'other'}, seq: 2),
      );
      expect(state.dialog, isNotNull, reason: 'a different dialog timed out');

      state = applyFrame(
        state,
        event('server.dialog.timeout', {'requestId': 'u1'}, seq: 3),
      );
      expect(state.dialog, isNull);
    });
  });

  group('tolerance', () {
    test('an unknown event and an unknown record change nothing', () {
      const initial = ChatState();
      expect(
        applyFrame(initial, event('future.something', {'x': 1})).entries,
        isEmpty,
      );
      expect(
        applyFrame(
          initial,
          entryEvent(<String, dynamic>{
            'id': 'e1',
            'type': 'brand-new-record-type',
          }),
        ).entries,
        isEmpty,
      );
      expect(
        applyFrame(initial, event('server.heartbeat', const {})).entries,
        isEmpty,
      );
    });

    test('a pi.unknown line is shown instead of dropped', () {
      final state = applyFrame(
        const ChatState(),
        event('pi.unknown', {'raw': 'this is not json'}),
      );
      expect((state.entries.single as StatusEntry).text, contains('not json'));
    });

    test('an exit and a crash become status lines', () {
      var state = applyFrame(
        const ChatState(),
        event('server.exited', {'exitCode': 0}),
      );
      expect((state.entries.single as StatusEntry).kind, StatusKind.info);
      state = applyFrame(
        state,
        event('server.crashed', {'exitCode': 137}, seq: 2),
      );
      final crash = state.entries.last as StatusEntry;
      expect(crash.kind, StatusKind.error);
      expect(crash.text, contains('137'));
    });

    test('a bridge notification becomes a status line', () {
      final state = applyFrame(
        const ChatState(),
        event('ext.notify', {
          'title': 'pi-ui-bridge',
          'message': 'goal reached',
        }),
      );
      expect((state.entries.single as StatusEntry).text, 'goal reached');
    });
  });
}
