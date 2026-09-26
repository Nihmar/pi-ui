import 'dart:async';

import 'package:stream_channel/stream_channel.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// A WebSocket the tests drive from both ends.
///
/// The client under test sees [stream] (what the server "sends") and writes
/// through [sink] into [sent]; the test itself calls [serverSend], [serverClose]
/// and reads [sent] to assert what the client put on the wire.
class FakeChannel with StreamChannelMixin<Object?> implements WebSocketChannel {
  FakeChannel({this.failReady = false});

  /// Makes [ready] fail, which is what a refused handshake looks like.
  final bool failReady;

  final _incoming = StreamController<Object?>.broadcast();
  final _outgoing = StreamController<Object?>.broadcast();

  /// Every frame the client sent, decoded.
  final List<Object?> sent = [];

  var _closed = false;

  /// Pushes a frame towards the client.
  void serverSend(Object? data) {
    if (!_incoming.isClosed) {
      _incoming.add(data);
    }
  }

  /// Ends the stream, as a dropped or refused connection does.
  Future<void> serverClose() async {
    if (!_incoming.isClosed) {
      await _incoming.close();
    }
  }

  @override
  Stream<Object?> get stream => _incoming.stream;

  @override
  WebSocketSink get sink => _sink ??= _FakeSink(this);

  WebSocketSink? _sink;

  @override
  Future<void> get ready => failReady
      ? Future<void>.error(StateError('handshake refused'))
      : Future<void>.value();

  @override
  String? get protocol => null;

  @override
  int? get closeCode => _closed ? 1000 : null;

  @override
  String? get closeReason => null;

  /// True once the client closed the sink.
  bool get closed => _closed;

  void _send(Object? data) => sent.add(data);

  Future<void> _close() async {
    _closed = true;
    // The outgoing side is only a recorder: closing it must never wait for a
    // listener that no test installed.
    if (!_outgoing.isClosed) {
      unawaited(_outgoing.close());
    }
  }
}

class _FakeSink implements WebSocketSink {
  _FakeSink(this._channel);

  final FakeChannel _channel;

  @override
  void add(Object? data) => _channel._send(data);

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future<void> addStream(Stream<Object?> stream) => stream.forEach(add);

  @override
  Future<void> close([int? closeCode, String? closeReason]) =>
      _channel._close();

  @override
  Future<void> get done => Future<void>.value();
}
