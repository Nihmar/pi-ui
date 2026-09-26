import 'json.dart';

/// The error taxonomy of `schemas/core.json`.
///
/// Clients branch on the code, never on the message (docs/api-v1.md, "Errors"), so
/// every failure the client surfaces carries one of these strings, including the
/// ones the client raises itself ([unreachable], [offline], [timeout]).
abstract final class ErrorCodes {
  /// Missing, unknown, expired or revoked credential; a bad pairing code.
  static const unauthorized = 'unauthorized';

  /// The device's scope does not cover the operation.
  static const forbiddenScope = 'forbidden_scope';

  /// The device or session limit is reached.
  static const deviceLimit = 'device_limit';
  static const sessionLimit = 'session_limit';

  /// A budget was exceeded; `retryAfter` says for how long.
  static const rateLimited = 'rate_limited';

  /// The request or frame was not readable.
  static const badRequest = 'bad_request';
  static const tooLarge = 'too_large';

  /// The session id is unknown.
  static const sessionNotFound = 'session_not_found';

  /// The child pi process failed (a command it refused, a broken pipe).
  static const piError = 'pi_error';

  /// The server's own policy turned the capability off (`git.write`), which is not the
  /// same as this device lacking the scope.
  static const featureDisabled = 'feature_disabled';

  /// The server has no workspace, so it exposes no filesystem, git or terminal surface.
  static const unsupported = 'unsupported';

  /// The deployment manages that itself (`POST /updates/apply`).
  static const managedMode = 'managed_mode';

  /// A dialog id that was never open or is already answered.
  static const notFound = 'not_found';
  static const alreadyAnswered = 'already_answered';

  /// The server is draining or shutting down.
  static const unavailable = 'unavailable';

  /// Client-side: the server could not be reached at all.
  static const unreachable = 'unreachable';

  /// Client-side: the connection dropped while the request was in flight.
  static const offline = 'offline';

  /// Client-side: no answer within the caller's budget.
  static const timeout = 'timeout';

  /// Client-side: a WebSocket frame the client could not parse.
  static const badFrame = 'bad_frame';

  /// The code is a client-only failure (the server never sends it).
  static bool isClientSide(String code) =>
      code == unreachable ||
      code == offline ||
      code == timeout ||
      code == badFrame;

  /// A sentence a user reads when the server sent no message.
  static String describe(String code) => switch (code) {
    unauthorized => 'The device token is not valid any more. Pair again.',
    forbiddenScope => 'This device is not allowed to do that.',
    deviceLimit => 'No free device slot: revoke one on the server first.',
    sessionLimit => 'The server reached its session limit.',
    rateLimited => 'Too many requests: wait a moment and retry.',
    badRequest => 'The server could not read the request.',
    tooLarge => 'The payload is too large for the server.',
    sessionNotFound => 'That session does not exist any more.',
    piError => 'pi refused the command.',
    notFound => 'That is not there any more.',
    alreadyAnswered => 'Somebody answered first.',
    unavailable => 'The server is shutting down.',
    featureDisabled => 'The server has that capability turned off.',
    unsupported => 'This server does not offer that.',
    managedMode => 'This deployment manages that itself.',
    unreachable => 'The server is unreachable.',
    offline => 'The connection dropped.',
    timeout => 'The server did not answer in time.',
    badFrame => 'The server sent a frame this client cannot read.',
    _ => 'The request failed.',
  };
}

/// One failure with a code a caller can branch on.
class PiuiException implements Exception {
  const PiuiException(this.code, this.message, {this.status, this.retryAfter});

  /// The taxonomy code ([ErrorCodes]).
  final String code;

  /// What the server said, or a sentence from [ErrorCodes.describe].
  final String message;

  /// The HTTP status, when the failure came from REST.
  final int? status;

  /// How long the caller should wait after a `rate_limited`.
  final Duration? retryAfter;

  /// True when retrying the same call may work without the user doing anything.
  bool get isTransient =>
      code == ErrorCodes.rateLimited ||
      code == ErrorCodes.unavailable ||
      code == ErrorCodes.unreachable ||
      code == ErrorCodes.offline ||
      code == ErrorCodes.timeout;

  /// True when the device must pair again.
  bool get isUnauthorized => code == ErrorCodes.unauthorized;

  @override
  String toString() => 'PiuiException($code, $message)';
}

/// Reads the `{"error":{"code","message"}}` envelope, or null when there is none.
PiuiException? errorFromBody(
  Object? body, {
  int? status,
  Duration? retryAfter,
}) {
  final error = asMap(asMap(body)?['error']);
  if (error == null) {
    return null;
  }
  final code = str(error['code'], fallback: ErrorCodes.badRequest);
  final message = str(error['message'], fallback: ErrorCodes.describe(code));
  return PiuiException(code, message, status: status, retryAfter: retryAfter);
}

/// Parses a `Retry-After` header: seconds, or null when absent or not a number.
Duration? retryAfterOf(String? header) {
  final seconds = header == null ? null : int.tryParse(header.trim());
  if (seconds == null || seconds <= 0) {
    return null;
  }
  return Duration(seconds: seconds);
}
