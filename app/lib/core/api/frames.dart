import 'dart:convert';

import '../models/chat_entry.dart';
import 'dto.dart';
import 'errors.dart';
import 'json.dart';

/// One frame of `/ws/v1` (docs/ws-protocol.md, "Server frames").
///
/// The server keeps `type` for the frame kind and for the event name (an event is
/// `pi.message_update`, a frame is `welcome`), so parsing dispatches on the four
/// reserved frame names and treats everything else as an event. An unknown event
/// name is a [WsEvent] like any other: that is the lenience the protocol promises
/// ("a client must ignore an unknown event name").
sealed class WsFrame {
  const WsFrame();

  /// The `type` field on the wire.
  String get type;
}

/// `{"type":"welcome",…}` — once per connection.
final class WsWelcome extends WsFrame {
  const WsWelcome({
    required this.version,
    required this.identity,
    required this.heartbeatSec,
  });

  final int version;
  final ServerIdentity identity;
  final int heartbeatSec;

  @override
  String get type => 'welcome';
}

/// `{"type":"pi.*|server.*|ext.*",…}` — one event of the stream.
final class WsEvent extends WsFrame {
  const WsEvent({
    required this.type,
    required this.seq,
    this.sessionId,
    this.entryId,
    this.at,
    this.payload = const {},
  });

  @override
  final String type;

  /// The session this event belongs to; null for server-wide facts.
  final String? sessionId;

  /// The hub's global ordering counter (starting at 1).
  final int seq;

  /// The durable cursor of this event, when it carries one.
  final String? entryId;

  final DateTime? at;

  /// The event's own payload, as it arrived on the wire.
  final Map<String, dynamic> payload;

  /// True for the frames that are not cursors: the heartbeat and the replay
  /// markers carry a `seq` but must never advance `since.seq`.
  bool get isMeta =>
      type == 'server.heartbeat' ||
      type == 'server.replay.begin' ||
      type == 'server.replay.end';
}

/// `{"type":"request",…}` — a blocking extension dialog.
final class WsRequest extends WsFrame {
  const WsRequest({
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
  final String sessionId;
  final DialogMethod method;
  final String title;
  final String? message;
  final List<String> options;
  final String? placeholder;
  final String? prefill;
  final DateTime at;

  /// When the server cancels the dialog on its own.
  final DateTime expiresAt;

  @override
  String get type => 'request';

  /// The dialog as the UI model.
  DialogRequest toDialogRequest() => DialogRequest(
    id: id,
    sessionId: sessionId,
    method: method,
    title: title,
    message: message,
    options: options,
    placeholder: placeholder,
    prefill: prefill,
    at: at,
    expiresAt: expiresAt,
  );
}

/// `{"type":"response",…}` — the answer to one `command`.
final class WsResponse extends WsFrame {
  const WsResponse({
    required this.id,
    required this.ok,
    this.data,
    this.errorCode,
    this.errorMessage,
  });

  final String id;
  final bool ok;
  final Map<String, dynamic>? data;
  final String? errorCode;
  final String? errorMessage;

  @override
  String get type => 'response';

  /// The failure this response carries, or null when it succeeded.
  PiuiException? get failure => ok
      ? null
      : PiuiException(
          errorCode ?? ErrorCodes.badRequest,
          errorMessage ??
              ErrorCodes.describe(errorCode ?? ErrorCodes.badRequest),
        );
}

/// `{"type":"pong"}` — the answer to a `ping`.
final class WsPong extends WsFrame {
  const WsPong();

  @override
  String get type => 'pong';
}

/// A frame this client has no type for: kept, never acted on.
final class WsUnknown extends WsFrame {
  const WsUnknown(this.type, this.raw);

  @override
  final String type;

  /// The frame exactly as it arrived.
  final String raw;
}

/// Parses one text frame. Throws [PiuiException] with `bad_frame` when the text
/// is not a JSON object, because a frame that cannot be read is a protocol
/// failure and not an event to ignore.
WsFrame parseFrame(String text) {
  final Object? decoded;
  try {
    decoded = jsonDecode(text);
  } on FormatException catch (error) {
    throw PiuiException(ErrorCodes.badFrame, 'Not JSON: ${error.message}');
  }
  final json = asMap(decoded);
  if (json == null) {
    throw const PiuiException(
      ErrorCodes.badFrame,
      'The frame is not a JSON object.',
    );
  }
  final type = str(json['type'], fallback: 'unknown');
  return switch (type) {
    'welcome' => WsWelcome(
      version: intOf(json['v'], fallback: 1),
      identity: ServerIdentity.fromJson(asMap(json['server']) ?? const {}),
      heartbeatSec: intOf(json['heartbeatSec'], fallback: 30),
    ),
    'request' => _request(json),
    'response' => WsResponse(
      id: str(json['id']),
      ok: boolOf(json['ok']),
      data: asMap(json['data']),
      errorCode: optStr(asMap(json['error'])?['code']),
      errorMessage: optStr(asMap(json['error'])?['message']),
    ),
    'pong' => const WsPong(),
    _ when type.contains('.') => WsEvent(
      type: type,
      sessionId: optStr(json['sessionId']),
      seq: intOf(json['seq']),
      entryId: optStr(json['entryId']),
      at: timeOf(json['ts']),
      payload: asMap(json['payload']) ?? const {},
    ),
    _ => WsUnknown(type, text),
  };
}

WsRequest _request(Map<String, dynamic> json) {
  final at = timeOf(json['ts']) ?? DateTime.now();
  final timeoutMs = intOf(json['timeoutMs']);
  return WsRequest(
    id: str(json['id']),
    sessionId: str(json['sessionId']),
    method: dialogMethodFrom(json['method']),
    title: str(json['title']),
    message: optStr(json['message']),
    options: [
      for (final option in asList(json['options']))
        if (option is String) option,
    ],
    placeholder: optStr(json['placeholder']),
    prefill: optStr(json['prefill']),
    at: at,
    expiresAt: at.add(Duration(milliseconds: timeoutMs)),
  );
}
