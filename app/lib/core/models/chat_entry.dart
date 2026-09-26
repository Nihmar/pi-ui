/// The chat timeline of the mockup: one entry per thing a session produced.
///
/// The shapes mirror what the server's event stream carries (message, tool call,
/// status, error), plus the client-only pieces the UI needs (streaming flags,
/// queue items, dialog requests).
library;

/// State of one tool call in the timeline.
enum ToolStatus {
  pending,
  running,
  success,
  error,
  denied;

  /// The word the UI shows on the card.
  String get label => switch (this) {
    ToolStatus.pending => 'waiting',
    ToolStatus.running => 'running',
    ToolStatus.success => 'done',
    ToolStatus.error => 'failed',
    ToolStatus.denied => 'denied',
  };
}

/// Severity of a status line in the timeline.
enum StatusKind { info, success, warning, error }

/// One line of a diff preview: unchanged, added or removed.
enum DiffLineKind { context, added, removed }

/// One diff line.
class DiffLine {
  const DiffLine(this.kind, this.text);

  final DiffLineKind kind;
  final String text;
}

/// A unified-diff preview of what a tool changed.
class DiffPreview {
  const DiffPreview({
    required this.path,
    required this.lines,
    this.added = 0,
    this.removed = 0,
  });

  final String path;
  final List<DiffLine> lines;
  final int added;
  final int removed;
}

/// One entry of the conversation timeline.
sealed class ChatEntry {
  const ChatEntry({required this.id, required this.at});

  /// Stable id: the client keys widget state (expanded cards, scroll anchors) by it.
  final String id;

  /// When the entry was appended.
  final DateTime at;
}

/// A message the user sent.
final class UserMessage extends ChatEntry {
  const UserMessage({
    required super.id,
    required super.at,
    required this.text,
    this.images = const [],
    this.queued = false,
  });

  final String text;

  /// Number of attached images (the mockup shows a chip, not the bytes).
  final List<String> images;

  /// True while the message is still in the offline queue.
  final bool queued;
}

/// A message the assistant produced.
final class AssistantMessage extends ChatEntry {
  const AssistantMessage({
    required super.id,
    required super.at,
    required this.text,
    this.thinking,
    this.streaming = false,
  });

  final String text;

  /// The reasoning block, when the model emitted one.
  final String? thinking;

  /// True while deltas are still arriving.
  final bool streaming;

  /// A copy the streaming scenario uses for its deltas.
  AssistantMessage copyWith({String? text, String? thinking, bool? streaming}) {
    return AssistantMessage(
      id: id,
      at: at,
      text: text ?? this.text,
      thinking: thinking ?? this.thinking,
      streaming: streaming ?? this.streaming,
    );
  }
}

/// A tool call the assistant made, with its arguments, output and optional diff.
final class ToolCallEntry extends ChatEntry {
  const ToolCallEntry({
    required super.id,
    required super.at,
    required this.name,
    required this.title,
    this.command,
    this.status = ToolStatus.pending,
    this.output,
    this.diff,
    this.duration,
    this.fullOutputPath,
  });

  /// Tool name (`bash`, `edit`, `read`, …).
  final String name;

  /// One-line description shown on the card.
  final String title;

  /// The command or arguments, shown in the card header.
  final String? command;

  final ToolStatus status;

  /// Captured output (stdout, file excerpt, tool result).
  final String? output;

  /// What the tool changed, when it changed a file.
  final DiffPreview? diff;

  /// How long the call took.
  final Duration? duration;

  /// Where the full output was saved, when the server truncated it.
  final String? fullOutputPath;

  ToolCallEntry copyWith({
    ToolStatus? status,
    String? output,
    DiffPreview? diff,
    Duration? duration,
    String? fullOutputPath,
  }) {
    return ToolCallEntry(
      id: id,
      at: at,
      name: name,
      title: title,
      command: command,
      status: status ?? this.status,
      output: output ?? this.output,
      diff: diff ?? this.diff,
      duration: duration ?? this.duration,
      fullOutputPath: fullOutputPath ?? this.fullOutputPath,
    );
  }
}

/// A lifecycle/status line: "compacting context", "crashed", "session limit".
final class StatusEntry extends ChatEntry {
  const StatusEntry({
    required super.id,
    required super.at,
    required this.text,
    this.kind = StatusKind.info,
    this.spinner = false,
  });

  final String text;
  final StatusKind kind;

  /// True while the line describes work in progress.
  final bool spinner;
}

/// A failure with a code a client can branch on (§11 of the interface contract).
final class ErrorEntry extends ChatEntry {
  const ErrorEntry({
    required super.id,
    required super.at,
    required this.title,
    required this.message,
    this.code,
    this.actionLabel,
  });

  final String title;
  final String message;

  /// The taxonomy code, when the server sent one.
  final String? code;

  /// An optional retry action offered by the UI.
  final String? actionLabel;
}

/// One queued message: a steer or a follow-up.
final class QueueItem {
  const QueueItem({required this.id, required this.kind, required this.text});

  final String id;

  /// `steer` or `follow_up`, as the `session.steer` / `session.follow_up` ops.
  final String kind;

  final String text;
}

/// The four answerable extension-UI methods the server routes as `request` frames.
enum DialogMethod {
  select,
  confirm,
  input,
  editor;

  /// The word the UI shows.
  String get label => switch (this) {
    DialogMethod.select => 'Choose',
    DialogMethod.confirm => 'Approve',
    DialogMethod.input => 'Answer',
    DialogMethod.editor => 'Edit',
  };
}

/// A blocking extension dialog waiting for an answer.
final class DialogRequest {
  const DialogRequest({
    required this.id,
    required this.sessionId,
    required this.method,
    required this.title,
    required this.at,
    required this.expiresAt,
    this.message,
    this.options = const [],
    this.placeholder,
    this.prefill,
  });

  /// pi's request id; the answer echoes it.
  final String id;

  /// The session that raised the dialog.
  final String sessionId;

  final DialogMethod method;
  final String title;
  final String? message;
  final List<String> options;
  final String? placeholder;
  final String? prefill;
  final DateTime at;

  /// When the server answers `cancelled:true` if nobody did.
  final DateTime expiresAt;

  /// Seconds left before the dialog cancels itself, floored at zero.
  int remainingSeconds(DateTime now) {
    final left = expiresAt.difference(now).inMilliseconds;
    return left <= 0 ? 0 : (left / 1000).ceil();
  }
}
