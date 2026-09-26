import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'errors.dart';
import 'frames.dart';
import 'http.dart';
import 'profile.dart';

/// How a socket is built, so a test can hand in a fake channel.
typedef ChannelFactory = WebSocketChannel Function(
  Uri uri,
  Map<String, String> headers,
);

/// The lifetime of the connection, as the UI banner renders it.
enum SocketStatus {
  /// Never started, or stopped on purpose (logout, app closed).
  idle,

  /// A first connection is being established.
  connecting,

  /// Connected, handshake done, frames flowing.
  online,

  /// Was connected, lost it, retrying with backoff.
  reconnecting;

  /// True while the client can send commands.
  bool get isOnline => this == SocketStatus.online;

  /// True while the client is waiting to get back.
  bool get isWorking => this == connecting || this == reconnecting;
}

/// The WebSocket half of the server API: one connection, the hello/welcome
/// handshake, subscriptions with replay cursors, keepalive and reconnection with
/// exponential backoff.
///
/// Reconnection is not "try again": every subscription remembers the last `seq`
/// and `entryId` it saw, so a reconnected client asks for exactly the gap it
/// missed (docs/ws-protocol.md, "Subscriptions and replay"). Meta frames never
/// advance the cursor — that is what keeps a replay from cutting off its own tail.
class PiUiSocket {
  PiUiSocket({
    required ServerProfile profile,
    ChannelFactory? channels,
    this.clientName = 'pi-ui-client',
    this.clientVersion = '0.1.0',
    this.handshakeTimeout = const Duration(seconds: 10),
    this.commandTimeout = const Duration(seconds: 30),
    this.baseBackoff = const Duration(milliseconds: 500),
    this.maxBackoff = const Duration(seconds: 30),
    this._jitter = true,
    Random? random,
    this.onProtocolError,
  }) : profile = profile,
       _channels =
           channels ??
           ((uri, headers) => defaultPiuiChannel(
             uri,
             headers,
             fingerprint: profile.fingerprint,
           )),
       _random = random ?? Random();

  /// The server this socket belongs to.
  final ServerProfile profile;

  final String clientName;
  final String clientVersion;
  final Duration handshakeTimeout;
  final Duration commandTimeout;
  final Duration baseBackoff;
  final Duration maxBackoff;

  /// Called for a frame the client could not read; the connection stays up.
  final void Function(PiuiException error)? onProtocolError;

  final ChannelFactory _channels;
  final bool _jitter;
  final Random _random;

  final _frames = StreamController<WsFrame>.broadcast();
  final _statuses = StreamController<SocketStatus>.broadcast();
  final _subscriptions = <String, _Subscription>{};
  final _pending = <String, Completer<Map<String, dynamic>?>>{};

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _subscription;
  Timer? _ping;
  Timer? _watchdog;
  Timer? _retry;
  Completer<void>? _online;
  SocketStatus _status = SocketStatus.idle;
  bool _stopped = true;
  bool _connecting = false;
  bool _everConnected = false;
  int _attempt = 0;
  int _commandSeq = 0;
  DateTime _lastFrameAt = DateTime.now();

  /// Every server frame except the handshake and the command answers.
  Stream<WsFrame> get frames => _frames.stream;

  /// Every status change, including the current one on listen.
  Stream<SocketStatus> get statusChanges async* {
    yield _status;
    yield* _statuses.stream;
  }

  /// The status right now.
  SocketStatus get status => _status;

  /// Connects (idempotent). Safe to call on every app start and resume.
  void start() {
    if (!_stopped) {
      return;
    }
    _stopped = false;
    unawaited(_connect());
  }

  /// Closes the connection and stays closed until [start] is called again.
  Future<void> stop() async {
    _stopped = true;
    _retry?.cancel();
    _clearHeartbeat();
    await _teardown();
    // The cursors are kept: a later start re-subscribes from where it stopped.
    _setStatus(SocketStatus.idle);
  }

  /// Drops the current connection and reconnects at once (app resumed, retry tap).
  Future<void> reconnectNow() async {
    if (_stopped) {
      start();
      return;
    }
    _retry?.cancel();
    await _teardown();
    _attempt = 0;
    await _connect();
  }

  /// Subscribes to one session.
  ///
  /// [replay] asks for the history when this subscription has no cursor yet; a
  /// subscription that already saw events resumes from the gap instead.
  void subscribe(String sessionId, {bool replay = true}) {
    final subscription = _subscriptions.putIfAbsent(
      sessionId,
      () => _Subscription(),
    );
    subscription.wanted = true;
    _sendSubscribe(sessionId, subscription, replay: replay);
  }

  /// Stops receiving one session's events. Idempotent.
  void unsubscribe(String sessionId) {
    _subscriptions.remove(sessionId);
    _send({'type': 'unsubscribe', 'sessionId': sessionId});
  }

  /// Sends one op (`session.prompt`, `session.steer`, …) and waits for its answer.
  ///
  /// [waitForConnection] makes a send issued while reconnecting wait instead of
  /// failing: the composer uses it so a prompt typed during a blip is not lost.
  Future<Map<String, dynamic>?> command({
    required String sessionId,
    required String op,
    Map<String, dynamic>? payload,
    Duration? timeout,
    Duration? waitForConnection,
  }) async {
    if (!_status.isOnline) {
      final budget = waitForConnection;
      if (budget == null) {
        throw const PiuiException(
          ErrorCodes.offline,
          'Not connected to the server.',
        );
      }
      await _awaitOnline(budget);
    }
    final id = 'c${++_commandSeq}';
    final completer = Completer<Map<String, dynamic>?>();
    _pending[id] = completer;
    try {
      _send({
        'type': 'command',
        'id': id,
        'sessionId': sessionId,
        'op': op,
        'payload': ?payload,
      });
    } catch (error) {
      _pending.remove(id);
      throw PiuiException(ErrorCodes.offline, '$error');
    }
    final response = await completer.future.timeout(
      timeout ?? commandTimeout,
      onTimeout: () {
        _pending.remove(id);
        throw const PiuiException(
          ErrorCodes.timeout,
          'The server did not answer the command.',
        );
      },
    );
    return response;
  }

  /// Answers one extension dialog.
  void uiResponse({
    required String sessionId,
    required String requestId,
    String? value,
    bool? confirmed,
    bool? cancelled,
  }) {
    _send({
      'type': 'ui_response',
      'sessionId': sessionId,
      'id': requestId,
      'value': ?value,
      'confirmed': ?confirmed,
      'cancelled': ?cancelled,
    });
  }

  /// Sends a keepalive ping.
  void ping() => _send({'type': 'ping'});

  /// Closes everything; the socket is unusable afterwards.
  Future<void> dispose() async {
    await stop();
    _failPending(const PiuiException(ErrorCodes.offline, 'The socket closed.'));
    await _frames.close();
    await _statuses.close();
  }

  Future<void> _connect() async {
    if (_stopped || _connecting || _status.isOnline) {
      return;
    }
    _connecting = true;
    _setStatus(
      _everConnected ? SocketStatus.reconnecting : SocketStatus.connecting,
    );
    try {
      final channel = _channels(profile.wsUri, {
        'Authorization': 'Bearer ${profile.token ?? ''}',
        'X-Piui-Protocol': '1',
      });
      _channel = channel;
      await channel.ready.timeout(handshakeTimeout);
      // The socket is up: from here on a failure is a lost connection, not a
      // first attempt, and the banner says "retrying" instead of "connecting".
      _everConnected = true;

      final welcome = Completer<WsWelcome>();
      _welcome = welcome;
      _subscription = channel.stream.listen(
        _onData,
        onError: (Object error) => _disconnected('$error'),
        onDone: () => _disconnected('The server closed the connection.'),
        cancelOnError: false,
      );
      _send({
        'type': 'hello',
        'v': ServerIdentityProtocol.value,
        'client': {'name': clientName, 'version': clientVersion},
      });
      final answer = await welcome.future.timeout(handshakeTimeout);
      if (answer.version != ServerIdentityProtocol.value) {
        _disconnected(
          'The server speaks WebSocket protocol ${answer.version}; '
          'this client speaks ${ServerIdentityProtocol.value}.',
        );
        return;
      }
      _welcome = null;
      _attempt = 0;
      _lastFrameAt = DateTime.now();
      _startHeartbeat(answer.heartbeatSec);
      _setStatus(SocketStatus.online);
      _online?.complete();
      _resubscribe();
    } catch (error) {
      _disconnected('$error');
    } finally {
      _connecting = false;
    }
  }

  Completer<WsWelcome>? _welcome;

  void _resubscribe() {
    for (final entry in _subscriptions.entries) {
      if (!entry.value.wanted) {
        continue;
      }
      _sendSubscribe(entry.key, entry.value, replay: true);
    }
  }

  void _sendSubscribe(
    String sessionId,
    _Subscription subscription, {
    required bool replay,
  }) {
    if (!_status.isOnline) {
      subscription.sent = false;
      return;
    }
    final cursor = subscription.cursor;
    // The whole history is requested once, by the first subscribe of this
    // session; every later one either resumes from the gap or asks for nothing.
    final fromScratch = cursor == null && replay && !subscription.bootstrapped;
    _send({
      'type': 'subscribe',
      'sessionId': sessionId,
      'since': ?cursor,
      if (fromScratch) 'replay': true,
    });
    subscription.sent = true;
    subscription.bootstrapped = true;
  }

  void _onData(dynamic data) {
    _lastFrameAt = DateTime.now();
    if (data is! String) {
      // Binary frames are ignored, not a reason to drop a working connection.
      return;
    }
    final WsFrame frame;
    try {
      frame = parseFrame(data);
    } on PiuiException catch (error) {
      onProtocolError?.call(error);
      return;
    }
    switch (frame) {
      case WsWelcome():
        final welcome = _welcome;
        if (welcome != null && !welcome.isCompleted) {
          welcome.complete(frame);
        }
      case WsResponse():
        final completer = _pending.remove(frame.id);
        if (completer == null) {
          break;
        }
        final failure = frame.failure;
        if (failure != null) {
          completer.completeError(failure);
        } else {
          completer.complete(frame.data);
        }
      case WsRequest():
        _frames.add(frame);
      case WsPong():
        break;
      case WsUnknown():
        break;
      case WsEvent():
        _advanceCursor(frame);
        _frames.add(frame);
    }
  }

  /// Advances the replay cursor of a session. Meta frames carry a `seq` too and
  /// must not move it: `replay.end` can be stamped after the events it wraps.
  void _advanceCursor(WsEvent event) {
    final sessionId = event.sessionId;
    if (sessionId == null || event.isMeta) {
      return;
    }
    final subscription = _subscriptions[sessionId];
    if (subscription == null) {
      return;
    }
    if (event.entryId != null) {
      subscription.entryId = event.entryId;
    }
    if (event.seq > 0) {
      subscription.seq = event.seq;
    }
  }

  void _startHeartbeat(int heartbeatSec) {
    _clearHeartbeat();
    final period = Duration(seconds: heartbeatSec < 1 ? 1 : heartbeatSec);
    _ping = Timer.periodic(period, (_) {
      if (!_status.isOnline) {
        return;
      }
      ping();
      // The server heartbeats on its own too; two periods without a single
      // frame means the connection looks alive and is not.
      if (DateTime.now().difference(_lastFrameAt) > period * 2.5) {
        _disconnected('No frame for ${period * 2.5}.');
      }
    });
    _watchdog = Timer.periodic(period, (_) {
      if (!_status.isOnline) {
        return;
      }
      if (DateTime.now().difference(_lastFrameAt) > period * 2.5) {
        _disconnected('The connection went silent.');
      }
    });
  }

  void _clearHeartbeat() {
    _ping?.cancel();
    _watchdog?.cancel();
    _ping = null;
    _watchdog = null;
  }

  /// Reports a lost connection and schedules the next attempt.
  void _disconnected(String reason) {
    _clearHeartbeat();
    unawaited(_teardown());
    _failPending(PiuiException(ErrorCodes.offline, reason));
    if (_stopped) {
      _setStatus(SocketStatus.idle);
      return;
    }
    _setStatus(SocketStatus.reconnecting);
    _scheduleReconnect();
  }

  void _scheduleReconnect() {
    _retry?.cancel();
    _attempt++;
    final delay = _backoffFor(_attempt);
    _retry = Timer(delay, () => unawaited(_connect()));
  }

  /// Exponential backoff with jitter: attempt 1 waits [baseBackoff], every
  /// further attempt doubles it up to [maxBackoff].
  Duration _backoffFor(int attempt) {
    final scale = 1 << (attempt - 1).clamp(0, 20);
    final raw = baseBackoff * scale;
    final capped = raw > maxBackoff ? maxBackoff : raw;
    if (!_jitter || capped.inMilliseconds < 20) {
      return capped;
    }
    final factor = 0.8 + _random.nextDouble() * 0.4;
    return Duration(
      microseconds: (capped.inMicroseconds * factor).round().clamp(
        1,
        maxBackoff.inMicroseconds,
      ),
    );
  }

  Future<void> _teardown() async {
    final subscription = _subscription;
    _subscription = null;
    await subscription?.cancel();
    final channel = _channel;
    _channel = null;
    if (channel != null) {
      try {
        await channel.sink.close();
      } catch (_) {
        // Closing an already dead socket is not an error worth reporting.
      }
    }
  }

  Future<void> _awaitOnline(Duration budget) async {
    if (_status.isOnline) {
      return;
    }
    final waiter = _online ??= Completer<void>();
    _online = waiter;
    try {
      await waiter.future.timeout(budget);
    } on TimeoutException {
      throw const PiuiException(
        ErrorCodes.offline,
        'Still reconnecting to the server.',
      );
    }
  }

  void _failPending(PiuiException error) {
    final pending = _pending.values.toList();
    _pending.clear();
    for (final completer in pending) {
      if (!completer.isCompleted) {
        completer.completeError(error);
      }
    }
    final waiter = _online;
    _online = null;
    if (waiter != null && !waiter.isCompleted) {
      waiter.completeError(error);
    }
  }

  void _send(Map<String, dynamic> frame) {
    final channel = _channel;
    if (channel == null || _stopped) {
      throw const PiuiException(
        ErrorCodes.offline,
        'Not connected to the server.',
      );
    }
    channel.sink.add(jsonEncode(frame));
  }

  void _setStatus(SocketStatus status) {
    if (status == _status) {
      return;
    }
    if (status == SocketStatus.online) {
      final waiter = _online;
      _online = null;
      if (waiter != null && !waiter.isCompleted) {
        waiter.complete();
      }
    }
    _status = status;
    if (!_statuses.isClosed) {
      _statuses.add(status);
    }
  }
}

/// One subscription's cursor: the last frame the client rendered.
class _Subscription {
  /// True while the caller wants this session's events.
  bool wanted = false;

  /// True while a subscribe frame is on the wire for this connection.
  bool sent = false;

  /// True once the history was asked for, so it is never asked twice.
  bool bootstrapped = false;

  /// The hub's global counter, for the in-memory ring.
  int? seq;

  /// The durable cursor, preferred over [seq] when it exists.
  String? entryId;

  /// The `since` object of the next subscribe frame.
  Map<String, Object>? get cursor {
    final id = entryId;
    if (id != null && id.isNotEmpty) {
      return {'entryId': id};
    }
    final number = seq;
    if (number != null) {
      return {'seq': number};
    }
    return null;
  }
}

/// The handshake version, kept apart from [PiUiClient] so the socket does not
/// import the REST layer.
abstract final class ServerIdentityProtocol {
  /// The protocol version this client speaks.
  static const value = 1;
}

/// The production channel: `IOWebSocketChannel` with the same pinned
/// `HttpClient` the REST client uses, so a certificate pin holds for both.
WebSocketChannel defaultPiuiChannel(
  Uri uri,
  Map<String, String> headers, {
  String? fingerprint,
}) => IOWebSocketChannel.connect(
  uri,
  headers: headers,
  customClient: piuiHttpClient(fingerprint: fingerprint),
  connectTimeout: const Duration(seconds: 10),
);
