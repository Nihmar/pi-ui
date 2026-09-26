import '../models/chat_entry.dart';
import 'frames.dart';
import 'json.dart';

/// The live state of one conversation: what the chat screen renders and what the
/// composer needs to know (is an answer streaming, what is queued, is a dialog
/// waiting).
class ChatState {
  const ChatState({
    this.entries = const [],
    this.queue = const [],
    this.dialog,
    this.streaming = false,
  });

  /// The timeline, oldest first.
  final List<ChatEntry> entries;

  /// Messages pi has queued but not processed (steering and follow-ups).
  final List<QueueItem> queue;

  /// The extension dialog waiting for an answer, if one is open.
  final DialogRequest? dialog;

  /// True while the assistant is producing an answer.
  final bool streaming;

  /// The id of the last entry, which is also the durable replay cursor.
  String? get lastEntryId => entries.isEmpty ? null : entries.last.id;

  ChatState copyWith({
    List<ChatEntry>? entries,
    List<QueueItem>? queue,
    DialogRequest? dialog,
    bool clearDialog = false,
    bool? streaming,
  }) => ChatState(
    entries: entries ?? this.entries,
    queue: queue ?? this.queue,
    dialog: clearDialog ? null : dialog ?? this.dialog,
    streaming: streaming ?? this.streaming,
  );
}

/// Applies one server frame to a conversation.
///
/// This is the whole mapping between pi's records and the timeline: a pure
/// function so it can be tested against captured frames without a server, a
/// socket or a widget. Unknown records and unknown event names change nothing —
/// that is how a newer server stays compatible with this client.
ChatState applyFrame(ChatState state, WsFrame frame) {
  return switch (frame) {
    WsRequest() => state.copyWith(dialog: frame.toDialogRequest()),
    WsEvent() => _applyEvent(state, frame),
    _ => state,
  };
}

ChatState _applyEvent(ChatState state, WsEvent event) {
  final payload = event.payload;
  return switch (event.type) {
    'pi.entry_appended' => _applyEntry(state, payload['entry']),
    'pi.message_update' => _applyMessageUpdate(state, payload),
    'pi.agent_end' => _finishStreaming(state),
    'pi.agent_settled' => _finishStreaming(state),
    'pi.queue_update' => state.copyWith(queue: _queueOf(payload)),
    'pi.compaction_start' => _append(
      state,
      StatusEntry(
        id: 'status:compaction:${event.seq}',
        at: event.at ?? DateTime.now(),
        text: 'Compacting the context…',
        spinner: true,
      ),
    ),
    'pi.compaction_end' => _append(
      state,
      StatusEntry(
        id: 'status:compaction-end:${event.seq}',
        at: event.at ?? DateTime.now(),
        text: _compactionText(payload),
        kind: boolOf(payload['aborted']) ? StatusKind.warning : StatusKind.info,
      ),
    ),
    'pi.auto_retry_start' => _append(
      state,
      StatusEntry(
        id: 'status:retry:${event.seq}',
        at: event.at ?? DateTime.now(),
        text:
            'Retrying (attempt ${intOf(payload['attempt'])}'
            '/${intOf(payload['maxAttempts'])}): '
            '${str(payload['errorMessage'])}',
        kind: StatusKind.warning,
        spinner: true,
      ),
    ),
    'pi.auto_retry_end' => _append(
      state,
      boolOf(payload['success'])
          ? StatusEntry(
              id: 'status:retry-end:${event.seq}',
              at: event.at ?? DateTime.now(),
              text: 'The retry worked.',
              kind: StatusKind.success,
            )
          : ErrorEntry(
              id: 'error:retry:${event.seq}',
              at: event.at ?? DateTime.now(),
              title: 'pi gave up retrying',
              message: str(
                payload['finalError'],
                fallback: 'The provider kept failing.',
              ),
            ),
    ),
    'pi.bash_execution_update' => _applyBashUpdate(state, event, payload),
    'ext.notify' => _append(
      state,
      StatusEntry(
        id: 'notify:${event.seq}',
        at: event.at ?? DateTime.now(),
        text: str(
          payload['message'],
          fallback: str(payload['title'], fallback: 'pi-ui-bridge'),
        ),
        kind: StatusKind.warning,
      ),
    ),
    'server.dialog.timeout' => _timeoutDialog(state, payload),
    'server.exited' || 'server.crashed' when event.sessionId != null => _append(
      state,
      StatusEntry(
        id: 'status:${event.type}:${event.seq}',
        at: event.at ?? DateTime.now(),
        text: event.type == 'server.crashed'
            ? 'The session crashed (exit ${intOf(payload['exitCode'], fallback: -1)}).'
            : 'The session exited (exit ${intOf(payload['exitCode'], fallback: 0)}).',
        kind: event.type == 'server.crashed'
            ? StatusKind.error
            : StatusKind.info,
      ),
    ),
    'pi.unknown' => _appendUnknown(state, event),
    _ => state,
  };
}

ChatState _timeoutDialog(ChatState state, Map<String, dynamic> payload) {
  final dialog = state.dialog;
  if (dialog == null || dialog.id != optStr(payload['requestId'])) {
    return state;
  }
  return state.copyWith(clearDialog: true);
}

ChatState _appendUnknown(ChatState state, WsEvent event) {
  final raw =
      optStr(event.payload['raw']) ?? optStr(event.payload['rawBase64']);
  if (raw == null) {
    return state;
  }
  return _append(
    state,
    StatusEntry(
      id: 'unknown:${event.seq}',
      at: event.at ?? DateTime.now(),
      text:
          'pi sent a line this client could not read: '
          '${raw.length > 120 ? '${raw.substring(0, 117)}…' : raw}',
      kind: StatusKind.warning,
    ),
  );
}

ChatState _applyEntry(ChatState state, Object? raw) {
  final entry = asMap(raw);
  if (entry == null) {
    return state;
  }
  final id = optStr(entry['id']) ?? 'entry:${entry.hashCode}';
  final at = timeOf(entry['timestamp']) ?? DateTime.now();
  return switch (str(entry['type'])) {
    'message' => _applyMessage(state, entry, id, at),
    'compaction' => _append(
      state,
      StatusEntry(
        id: id,
        at: at,
        text:
            'Context compacted: ${intOf(entry['tokensBefore'])} tokens '
            'summarized.',
      ),
    ),
    'custom' || 'custom_message' => _applyCustom(state, entry, id, at),
    _ => state,
  };
}

ChatState _applyMessage(
  ChatState state,
  Map<String, dynamic> entry,
  String id,
  DateTime at,
) {
  final message = asMap(entry['message']);
  if (message == null) {
    return state;
  }
  return switch (str(message['role'])) {
    'user' => _append(
      state,
      UserMessage(id: id, at: at, text: _textOf(message)),
    ),
    'assistant' => _applyAssistant(state, message, id, at),
    'toolResult' => _applyToolResult(state, message, id, at),
    'bashExecution' => _applyBashEntry(state, message, id, at),
    'compactionSummary' || 'branchSummary' => _append(
      state,
      StatusEntry(
        id: id,
        at: at,
        text: str(
          message['summary'],
          fallback: 'The conversation was summarized.',
        ),
      ),
    ),
    // System messages carry the prompt and the tool loadout: real context, but
    // not a conversation line the user asked to read.
    _ => state,
  };
}

/// A custom entry pi writes for an extension (goal mode, markers): shown as a
/// status line, because this client has no renderer for the extension's data.
ChatState _applyCustom(
  ChatState state,
  Map<String, dynamic> entry,
  String id,
  DateTime at,
) {
  final content = _textOf(asMap(entry) ?? const {});
  final label = optStr(entry['customType']) ?? 'custom';
  if (content.isEmpty) {
    return state;
  }
  return _append(state, StatusEntry(id: id, at: at, text: '$label: $content'));
}

ChatState _applyAssistant(
  ChatState state,
  Map<String, dynamic> message,
  String id,
  DateTime at,
) {
  final text = StringBuffer();
  final thinking = StringBuffer();
  final calls = <_ToolCall>[];
  for (final block in asMapList(message['content'])) {
    switch (str(block['type'])) {
      case 'text':
        text.write(str(block['text']));
      case 'thinking':
        thinking.write(str(block['thinking']));
      case 'toolCall':
        final call = _ToolCall.fromBlock(block);
        if (call != null) {
          calls.add(call);
        }
    }
  }
  final entries = [...state.entries];
  final spoken = text.toString();
  final reasoned = thinking.isEmpty ? null : thinking.toString();
  // The entry replaces the message the deltas built: same position, final text.
  final streamingId = _streamingId(state);
  final index = streamingId == null
      ? -1
      : entries.indexWhere((entry) => entry.id == streamingId);
  if (spoken.isEmpty && reasoned == null) {
    // A turn that only called tools: an empty bubble would be noise, so the
    // streaming placeholder goes and the cards below are the whole turn.
    if (index >= 0) {
      entries.removeAt(index);
    }
  } else {
    final assistant = AssistantMessage(
      id: id,
      at: at,
      text: spoken,
      thinking: reasoned,
    );
    if (index >= 0) {
      entries[index] = assistant;
    } else {
      entries.add(assistant);
    }
  }
  for (final call in calls) {
    // A card for this call may already exist from the deltas; keep that one.
    if (entries.any((entry) => entry.id == call.entryId)) {
      continue;
    }
    entries.add(
      ToolCallEntry(
        id: call.entryId,
        at: at,
        name: call.name,
        title: call.title,
        command: call.command,
      ),
    );
  }
  return state.copyWith(entries: entries, streaming: false);
}

ChatState _applyToolResult(
  ChatState state,
  Map<String, dynamic> message,
  String id,
  DateTime at,
) {
  final callId = optStr(message['toolCallId']) ?? id;
  final output = _textOf(message);
  final failed = boolOf(message['isError']);
  final entries = [...state.entries];
  final index = entries.indexWhere((entry) => entry.id == 'tool:$callId');
  if (index < 0) {
    // The call's card never arrived (a resumed session that starts mid-run):
    // the result is still worth showing.
    entries.add(
      ToolCallEntry(
        id: 'tool:$callId',
        at: at,
        name: str(message['toolName'], fallback: 'tool'),
        title: str(message['toolName'], fallback: 'tool'),
        status: failed ? ToolStatus.error : ToolStatus.success,
        output: output,
      ),
    );
  } else {
    final card = entries[index] as ToolCallEntry;
    entries[index] = card.copyWith(
      status: failed ? ToolStatus.error : ToolStatus.success,
      output: output,
    );
  }
  return state.copyWith(entries: entries);
}

ChatState _applyBashEntry(
  ChatState state,
  Map<String, dynamic> message,
  String id,
  DateTime at,
) {
  final exitCode = optInt(message['exitCode']);
  return _append(
    state,
    ToolCallEntry(
      id: id,
      at: at,
      name: 'bash',
      title: str(message['command'], fallback: 'bash'),
      command: str(message['command']),
      status: boolOf(message['cancelled'])
          ? ToolStatus.denied
          : exitCode == null || exitCode == 0
          ? ToolStatus.success
          : ToolStatus.error,
      output: str(message['output']),
      fullOutputPath: optStr(message['fullOutputPath']),
    ),
  );
}

ChatState _applyBashUpdate(
  ChatState state,
  WsEvent event,
  Map<String, dynamic> payload,
) {
  final id = 'bash:${optStr(payload['id']) ?? event.seq}';
  final delta = str(payload['delta']);
  final entries = [...state.entries];
  final index = entries.indexWhere((entry) => entry.id == id);
  if (index < 0) {
    entries.add(
      ToolCallEntry(
        id: id,
        at: event.at ?? DateTime.now(),
        name: 'bash',
        title: 'bash',
        status: ToolStatus.running,
        output: delta,
      ),
    );
  } else {
    final card = entries[index] as ToolCallEntry;
    entries[index] = card.copyWith(output: '${card.output ?? ''}$delta');
  }
  return state.copyWith(entries: entries);
}

ChatState _applyMessageUpdate(ChatState state, Map<String, dynamic> payload) {
  final update = asMap(payload['assistantMessageEvent']);
  if (update == null) {
    return state;
  }
  final kind = str(update['type']);
  final entries = [...state.entries];
  switch (kind) {
    case 'start':
    case 'text_start':
    case 'thinking_start':
      if (!state.streaming) {
        entries.add(
          AssistantMessage(
            id: _streamId,
            at: DateTime.now(),
            text: '',
            streaming: true,
          ),
        );
      }
      return state.copyWith(entries: entries, streaming: true);
    case 'text_delta':
      return _stream(state, entries, text: str(update['delta']));
    case 'thinking_delta':
      return _stream(state, entries, thinking: str(update['delta']));
    case 'toolcall_start':
      final callId = optStr(update['id']) ?? optStr(update['toolName']);
      if (callId == null) {
        return state;
      }
      final entryId = 'tool:$callId';
      if (!entries.any((entry) => entry.id == entryId)) {
        entries.add(
          ToolCallEntry(
            id: entryId,
            at: DateTime.now(),
            name: str(update['toolName'], fallback: 'tool'),
            title: str(update['toolName'], fallback: 'tool'),
            status: ToolStatus.running,
          ),
        );
      }
      return state.copyWith(entries: entries);
    case 'toolcall_end':
      final block = asMap(update['toolCall']);
      final call = block == null ? null : _ToolCall.fromBlock(block);
      if (call == null) {
        return state;
      }
      final index = entries.indexWhere((entry) => entry.id == call.entryId);
      if (index < 0) {
        entries.add(
          ToolCallEntry(
            id: call.entryId,
            at: DateTime.now(),
            name: call.name,
            title: call.title,
            command: call.command,
            status: ToolStatus.running,
          ),
        );
      } else {
        final card = entries[index] as ToolCallEntry;
        entries[index] = ToolCallEntry(
          id: card.id,
          at: card.at,
          name: call.name,
          title: call.title,
          command: call.command,
          status: ToolStatus.running,
        );
      }
      return state.copyWith(entries: entries);
    case 'done':
    case 'error':
      final finished = _finishStreaming(state.copyWith(entries: entries));
      if (kind == 'error') {
        final error = asMap(update['error']);
        final message = optStr(error?['errorMessage']);
        if (message != null) {
          return _append(
            finished,
            ErrorEntry(
              id: 'error:model:${DateTime.now().microsecondsSinceEpoch}',
              at: DateTime.now(),
              title: 'The model call failed',
              message: message,
            ),
          );
        }
      }
      return finished;
    default:
      return state;
  }
}

ChatState _stream(
  ChatState state,
  List<ChatEntry> entries, {
  String? text,
  String? thinking,
}) {
  var streaming = false;
  for (var index = 0; index < entries.length; index++) {
    final entry = entries[index];
    if (entry is! AssistantMessage || entry.id != _streamId) {
      continue;
    }
    entries[index] = entry.copyWith(
      text: '${entry.text}${text ?? ''}',
      thinking: thinking == null ? null : '${entry.thinking ?? ''}$thinking',
      streaming: true,
    );
    streaming = true;
  }
  if (!streaming) {
    entries.add(
      AssistantMessage(
        id: _streamId,
        at: DateTime.now(),
        text: text ?? '',
        thinking: thinking,
        streaming: true,
      ),
    );
  }
  return state.copyWith(entries: entries, streaming: true);
}

ChatState _finishStreaming(ChatState state) {
  if (!state.streaming) {
    return state;
  }
  final streamingId = _streamingId(state);
  final entries = [
    for (final entry in state.entries)
      if (entry is AssistantMessage &&
          entry.id == streamingId &&
          entry.streaming)
        entry.copyWith(streaming: false)
      else
        entry,
  ];
  return state.copyWith(entries: entries, streaming: false);
}

String? _streamingId(ChatState state) {
  for (final entry in state.entries) {
    if (entry is AssistantMessage && entry.streaming) {
      return entry.id;
    }
  }
  return null;
}

ChatState _append(ChatState state, ChatEntry entry) => state.copyWith(
  entries: [...state.entries, entry],
  streaming: state.streaming,
);

List<QueueItem> _queueOf(Map<String, dynamic> payload) {
  final queue = <QueueItem>[];
  final steering = asList(payload['steering']);
  for (var index = 0; index < steering.length; index++) {
    final text = steering[index];
    if (text is String && text.isNotEmpty) {
      queue.add(QueueItem(id: 'steer:$index', kind: 'steer', text: text));
    }
  }
  final followUps = asList(payload['followUp']);
  for (var index = 0; index < followUps.length; index++) {
    final text = followUps[index];
    if (text is String && text.isNotEmpty) {
      queue.add(QueueItem(id: 'follow:$index', kind: 'follow_up', text: text));
    }
  }
  return queue;
}

String _compactionText(Map<String, dynamic> payload) {
  if (boolOf(payload['aborted'])) {
    return 'Compaction was aborted.';
  }
  final result = asMap(payload['result']);
  final tokens = optInt(result?['tokensAfter']);
  return tokens == null
      ? 'Context compacted.'
      : 'Context compacted to $tokens tokens.';
}

/// The text of a message or entry whose content is a string or text blocks.
String _textOf(Map<String, dynamic> message) {
  final content = message['content'];
  if (content is String) {
    return content;
  }
  final buffer = StringBuffer();
  for (final block in asMapList(content)) {
    if (str(block['type']) == 'text') {
      if (buffer.isNotEmpty) {
        buffer.write('\n');
      }
      buffer.write(str(block['text']));
    }
  }
  // A custom entry keeps its text in `content` as a plain string.
  final direct = optStr(message['content']);
  return buffer.isEmpty ? direct ?? '' : buffer.toString();
}

/// The id the streaming assistant message uses until its entry is appended.
const _streamId = 'assistant:streaming';

/// One tool call, read out of an assistant content block.
class _ToolCall {
  const _ToolCall({
    required this.callId,
    required this.name,
    required this.title,
    this.command,
  });

  final String callId;
  final String name;
  final String title;
  final String? command;

  /// The timeline id of the card: stable across the deltas and the final entry.
  String get entryId => 'tool:$callId';

  static _ToolCall? fromBlock(Map<String, dynamic> block) {
    final callId = optStr(block['id']);
    final name = optStr(block['name']);
    if (callId == null || name == null) {
      return null;
    }
    final arguments = asMap(block['arguments']) ?? const {};
    return _ToolCall(
      callId: callId,
      name: name,
      title: _titleOf(name, arguments),
      command: optStr(arguments['command']),
    );
  }

  static String _titleOf(String name, Map<String, dynamic> arguments) {
    for (final key in const [
      'command',
      'file_path',
      'path',
      'pattern',
      'query',
      'url',
      'description',
    ]) {
      final value = optStr(arguments[key]);
      if (value != null) {
        return value;
      }
    }
    return name;
  }
}

/// Debug helper: the frame as one line, for logs and tests.
String frameSummary(WsFrame frame) => switch (frame) {
  WsEvent(:final type, :final sessionId, :final seq) =>
    '$type${sessionId == null ? '' : ' ($sessionId)'} #$seq',
  _ => frame.type,
};
