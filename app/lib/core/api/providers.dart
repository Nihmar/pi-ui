import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/chat_entry.dart';
import '../models/session.dart';
import '../notify.dart';
import 'client.dart';
import 'errors.dart';
import 'dto.dart';
import 'frames.dart';
import 'profile.dart';
import 'session_actions.dart';
import 'session_stream.dart';
import 'socket.dart';
import 'timeline.dart';

/// Where the server profile lives. Overridden in tests with
/// [MemoryProfileStore]; the app uses the preferences + keystore pair.
final profileStoreProvider = Provider<ProfileStore>(
  (ref) => PrefsProfileStore(),
);

/// Probes `GET /health` of a server that is not paired yet.
///
/// A function seam rather than a call site: the onboarding screen holds no
/// client, and a test drives the flow without a socket.
typedef HealthProbe = Future<bool> Function(String baseUrl);

/// Pairs this device with a server.
typedef Pairer = Future<PairResult> Function({
  required String baseUrl,
  required String deviceName,
  String? code,
  String? secret,
  String? password,
  String? fingerprint,
});

/// The real probe: a throw-away client, because there is no token yet.
final healthProbeProvider = Provider<HealthProbe>(
  (ref) =>
      (baseUrl) => PiUiClient(
        profile: ServerProfile(baseUrl: baseUrl, deviceName: 'probe'),
      ).health(),
);

/// The real pairing call (`POST /auth/pair`).
final pairerProvider = Provider<Pairer>(
  (ref) =>
      ({
        required String baseUrl,
        required String deviceName,
        String? code,
        String? secret,
        String? password,
        String? fingerprint,
      }) => PiUiClient.pair(
        baseUrl: baseUrl,
        deviceName: deviceName,
        code: code,
        secret: secret,
        password: password,
        fingerprint: fingerprint,
        platform: currentPlatform(),
      ),
);

/// The paired server, or null before the first pairing.
///
/// Everything else hangs off this provider: no profile means no client, no
/// socket and the onboarding route.
final profileProvider =
    AsyncNotifierProvider<ProfileController, ServerProfile?>(
      ProfileController.new,
    );

/// Loads, pairs, rotates and forgets the server profile.
class ProfileController extends AsyncNotifier<ServerProfile?> {
  @override
  Future<ServerProfile?> build() => ref.watch(profileStoreProvider).read();

  ProfileStore get _store => ref.read(profileStoreProvider);

  /// Pairs this device with a server and stores the token it handed back.
  ///
  /// Exactly one of [code], [secret] or [password] is the invitation; the
  /// profile keeps the server identity so the fingerprint stays pinned.
  Future<PairResult> pair({
    required String baseUrl,
    required String deviceName,
    String? code,
    String? secret,
    String? password,
    String? fingerprint,
  }) async {
    final normalized = normalizeServerUrl(baseUrl);
    final result = await ref.read(pairerProvider)(
      baseUrl: normalized,
      deviceName: deviceName,
      code: code,
      secret: secret,
      password: password,
      fingerprint: fingerprint,
    );
    final profile = ServerProfile(
      baseUrl: normalized,
      deviceName: deviceName,
      token: result.token,
      deviceId: result.deviceId,
      scope: result.scope,
      identity: result.server,
    );
    if (fingerprint != null && result.server.fingerprint == null) {
      // The user confirmed a fingerprint the server did not repeat: keep the
      // confirmation, it is the stronger statement of the two.
      await _store.write(
        profile.copyWith(identity: result.server.withFingerprint(fingerprint)),
      );
      state = AsyncData(
        profile.copyWith(identity: result.server.withFingerprint(fingerprint)),
      );
      return result;
    }
    await _store.write(profile);
    state = AsyncData(profile);
    return result;
  }

  /// Stores a rotated token, keeping the rest of the profile.
  Future<void> rotateToken(PairResult result) async {
    final updated = await _store.writeToken(result.token);
    final current = state.value;
    state = AsyncData(updated ?? current?.copyWith(token: result.token));
  }

  /// Renames this device's display name.
  Future<void> rename(String deviceName) async {
    final current = state.value;
    if (current == null) {
      return;
    }
    final updated = current.copyWith(deviceName: deviceName.trim());
    await _store.write(updated);
    state = AsyncData(updated);
  }

  /// Forgets the server: the next build of the app starts at onboarding.
  Future<void> logout() async {
    await _store.clear();
    state = const AsyncData(null);
  }

  /// Re-reads the server's identity (the settings screen's "refresh" action).
  Future<ServerIdentity?> refreshIdentity() async {
    final current = state.value;
    if (current == null || !current.isPaired) {
      return null;
    }
    final client = PiUiClient(profile: current);
    final identity = await client.identity();
    final updated = current.copyWith(identity: identity);
    await _store.write(updated);
    state = AsyncData(updated);
    return identity;
  }
}

/// The REST client of the paired server, or null when there is nothing to talk to.
final clientProvider = Provider<PiUiClient?>((ref) {
  final profile = ref.watch(profileProvider).value;
  if (profile == null || !profile.isPaired) {
    return null;
  }
  return PiUiClient(
    profile: profile,
    onTokenRotated: (result) {
      unawaited(ref.read(profileProvider.notifier).rotateToken(result));
    },
  );
});

/// The WebSocket of the paired server, connected and kept alive.
///
/// One socket per app, not per screen: the session list, the open chat and the
/// banner all read the same connection, and its replay cursors survive a screen
/// change. The provider disposes it when the profile changes.
final socketProvider = Provider<PiUiSocket?>((ref) {
  final profile = ref.watch(profileProvider).value;
  if (profile == null || !profile.isPaired) {
    return null;
  }
  final socket = PiUiSocket(profile: profile);
  ref.onDispose(() => unawaited(socket.dispose()));
  socket.start();
  return socket;
});

/// The OS notifier this build can use.
///
/// Every target pi-ui ships for has a notification centre, so the real notifier
/// is unconditional; the seam exists for tests and for a platform where the
/// plugin is unavailable (the app then just does not disturb anybody).
final appNotifierProvider = Provider<AppNotifier>((ref) {
  final notifier = LocalNotifier();
  ref.onDispose(() => unawaited(notifier.initialize()));
  return notifier;
});

/// Keeps the foreground state the watcher reads, and brings the app back to life
/// on resume: the socket reconnects at once instead of waiting out its backoff,
/// and the session list is re-read because a lot may have happened meanwhile.
final foregroundProvider = Provider<ForegroundState>((ref) {
  final state = ForegroundState(
    onResumed: () {
      unawaited(ref.read(socketProvider)?.reconnectNow());
      ref.invalidate(sessionsProvider);
    },
  );
  ref.onDispose(state.dispose);
  return state;
});

/// Notifies when a run ends while the app is in the background.
///
/// One watcher per app: it follows the same socket the screens do, so a session
/// the user never opened still tells them when it is done.
final sessionWatcherProvider = Provider<SessionWatcher?>((ref) {
  final socket = ref.watch(socketProvider);
  if (socket == null) {
    return null;
  }
  final notifier = ref.watch(appNotifierProvider);
  unawaited(notifier.initialize());
  final foreground = ref.watch(foregroundProvider);
  final watcher = SessionWatcher(
    frames: socket.frames,
    notifier: notifier,
    isForeground: () => foreground.isForeground,
    titleOf: (sessionId) =>
        ref.read(sessionProvider(sessionId))?.displayName ?? 'pi session',
  )..start();
  ref.onDispose(() => unawaited(watcher.dispose()));
  return watcher;
});

/// The driving operations of one session (prompt, steer, stop, rename, …).
final sessionActionsProvider = Provider<SessionActions?>((ref) {
  final socket = ref.watch(socketProvider);
  final client = ref.watch(clientProvider);
  if (socket == null || client == null) {
    return null;
  }
  return SessionActions(socket: socket, client: client);
});

/// The server's policy as the app reads it: today the theme, and the language a
/// settings screen will follow.
///
/// A failed read is not an error the app shows: it keeps its defaults, because a server
/// that cannot answer must not change how the app looks.
final serverSettingsProvider = FutureProvider<Map<String, dynamic>>((
  ref,
) async {
  final client = ref.watch(clientProvider);
  if (client == null) {
    return const <String, dynamic>{};
  }
  try {
    return await client.settings();
  } on PiuiException {
    return const <String, dynamic>{};
  }
});

/// The theme every client of this server shows, from `ui.theme`.
final themeModeProvider = Provider<ThemeMode>((ref) {
  final settings = ref.watch(serverSettingsProvider).value;
  return themeModeFrom(settings?['ui.theme']);
});

/// Maps the `ui.theme` value onto a [ThemeMode]: an unknown value follows the system,
/// which is the safe answer for a client that does not understand the server's choice.
ThemeMode themeModeFrom(Object? value) => switch (value) {
  'dark' => ThemeMode.dark,
  'light' => ThemeMode.light,
  _ => ThemeMode.system,
};

/// The connection state the banner renders.
final connectionProvider = StreamProvider<SocketStatus>((ref) {
  final socket = ref.watch(socketProvider);
  if (socket == null) {
    return Stream.value(SocketStatus.idle);
  }
  return socket.statusChanges;
});

/// Every session, live: seeded from REST, patched by the event stream.
final sessionsProvider = StreamProvider<List<SessionModel>>((ref) {
  final client = ref.watch(clientProvider);
  if (client == null) {
    return Stream.value(const <SessionModel>[]);
  }
  return watchSessions(client: client, socket: ref.watch(socketProvider));
});

/// One session out of [sessionsProvider].
final sessionProvider = Provider.family<SessionModel?, String>((ref, id) {
  final sessions = ref.watch(sessionsProvider).value ?? const <SessionModel>[];
  for (final session in sessions) {
    if (session.id == id) {
      return session;
    }
  }
  return null;
});

/// The live timeline of one session.
///
/// Subscribing happens when the screen listens, and the subscription is dropped
/// when it stops: the server replays the history on the next open, so the client
/// never has to cache a conversation (`AGENTS.md`: conversations live in pi's
/// own session files).
final chatStateProvider = StreamProvider.family<ChatState, String>((
  ref,
  sessionId,
) {
  final socket = ref.watch(socketProvider);
  if (socket == null) {
    return Stream.value(const ChatState());
  }
  final controller = StreamController<ChatState>();
  var state = const ChatState();
  final subscription = socket.frames.listen((frame) {
    if (!_belongsTo(frame, sessionId)) {
      return;
    }
    state = applyFrame(state, frame);
    if (!controller.isClosed) {
      controller.add(state);
    }
  }, onError: (Object _) {});
  controller
    ..onListen = () {
      socket.subscribe(sessionId);
      controller.add(state);
    }
    ..onCancel = () async {
      await subscription.cancel();
      socket.unsubscribe(sessionId);
    };
  return controller.stream;
});

/// The timeline entries of one session.
final chatProvider = Provider.family<AsyncValue<List<ChatEntry>>, String>(
  (ref, sessionId) => ref
      .watch(chatStateProvider(sessionId))
      .whenData((state) => state.entries),
);

/// The messages pi has queued for one session.
final queueProvider = Provider.family<AsyncValue<List<QueueItem>>, String>(
  (ref, sessionId) =>
      ref.watch(chatStateProvider(sessionId)).whenData((state) => state.queue),
);

/// The dialog one session waits on, if any.
final dialogProvider = Provider.family<AsyncValue<DialogRequest?>, String>(
  (ref, sessionId) =>
      ref.watch(chatStateProvider(sessionId)).whenData((state) => state.dialog),
);

/// True while the assistant of [sessionId] is producing an answer.
final streamingProvider = Provider.family<bool, String>(
  (ref, sessionId) =>
      ref.watch(chatStateProvider(sessionId)).value?.streaming ?? false,
);

/// The name of the platform this build runs on, as pairing reports it.
String currentPlatform() => switch (defaultTargetPlatform) {
  TargetPlatform.android => 'android',
  TargetPlatform.linux => 'linux',
  TargetPlatform.windows => 'windows',
  TargetPlatform.macOS => 'macos',
  TargetPlatform.iOS => 'ios',
  TargetPlatform.fuchsia => 'fuchsia',
};

bool _belongsTo(WsFrame frame, String wanted) => switch (frame) {
  WsEvent(:final sessionId) => sessionId == wanted,
  WsRequest(:final sessionId) => sessionId == wanted,
  _ => false,
};
