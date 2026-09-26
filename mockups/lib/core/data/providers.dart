import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/chat_entry.dart';
import '../models/scenario.dart';
import '../models/session.dart';
import 'mock_api.dart';

/// The fake server, one per app instance. `seed()` gives the reviewer two
/// sessions with history to open immediately.
final mockApiProvider = Provider<MockPiApi>((ref) {
  final api = MockPiApi()..seed();
  ref.onDispose(api.dispose);
  return api;
});

/// Every session, newest first.
final sessionsProvider = StreamProvider<List<SessionModel>>(
  (ref) => ref.watch(mockApiProvider).sessions,
);

/// One session, live.
final sessionProvider = StreamProvider.family<SessionModel?, String>((ref, id) {
  final api = ref.watch(mockApiProvider);
  return api.sessions.map((sessions) => _findSession(sessions, id));
});

/// The timeline of one session.
final chatProvider = StreamProvider.family<List<ChatEntry>, String>((ref, id) {
  return ref.watch(mockApiProvider).entriesOf(id);
});

/// The queued messages of one session.
final queueProvider = StreamProvider.family<List<QueueItem>, String>((ref, id) {
  return ref.watch(mockApiProvider).queueOf(id);
});

/// The dialog waiting in one session, if any.
final dialogProvider = StreamProvider.family<DialogRequest?, String>((ref, id) {
  return ref.watch(mockApiProvider).dialogOf(id);
});

/// The connection state the global banner renders.
final connectionProvider = StreamProvider<MockConnection>(
  (ref) => ref.watch(mockApiProvider).connection,
);

SessionModel? _findSession(List<SessionModel> sessions, String id) {
  for (final session in sessions) {
    if (session.id == id) {
      return session;
    }
  }
  return null;
}
