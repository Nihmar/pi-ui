import 'dart:async';

import '../models/session.dart';
import 'client.dart';
import 'session_store.dart';
import 'socket.dart';

/// The session list as a live stream: the REST list seeds it, every lifecycle
/// frame patches it, and a reconnect refreshes it once (a session created by
/// another device while this client was offline is not in the frame history).
///
/// Kept out of the Riverpod layer on purpose: a plain function over a client and
/// a socket is what the tests drive with a fake server.
Stream<List<SessionModel>> watchSessions({
  required PiUiClient client,
  required PiUiSocket? socket,
}) {
  late StreamController<List<SessionModel>> controller;
  StreamSubscription<dynamic>? frames;
  StreamSubscription<SocketStatus>? statuses;
  var sessions = const <SessionModel>[];
  var sawOnline = false;
  var closed = false;

  Future<void> refresh() async {
    try {
      final fetched = await client.sessions();
      sessions = fetched;
      if (!closed) {
        controller.add(fetched);
      }
    } catch (error, stack) {
      if (!closed) {
        controller.addError(error, stack);
      }
    }
  }

  controller = StreamController<List<SessionModel>>(
    onListen: () async {
      await refresh();
      final live = socket;
      if (live == null || closed) {
        return;
      }
      frames = live.frames.listen((frame) {
        final folded = applySessionFrame(sessions, frame);
        if (identical(folded, sessions)) {
          return;
        }
        sessions = folded;
        if (!closed) {
          controller.add(folded);
        }
      }, onError: (Object _) {});
      statuses = live.statusChanges.listen((status) {
        if (!status.isOnline) {
          return;
        }
        // The first online is what the seed above already covered; every later
        // one is a gap that may hide sessions this client never saw.
        if (sawOnline) {
          unawaited(refresh());
        }
        sawOnline = true;
      });
    },
    onCancel: () async {
      closed = true;
      await frames?.cancel();
      await statuses?.cancel();
      frames = null;
      statuses = null;
    },
  );
  return controller.stream;
}
