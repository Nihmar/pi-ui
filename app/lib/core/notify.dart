import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'api/frames.dart';

/// What the app tells the user about when they are not looking at it.
///
/// A seam, not a plugin call: the watcher below decides *when* something is
/// worth a notification, and this decides *how* one is shown. A test swaps in
/// [RecordingNotifier]; a platform without a notification channel gets
/// [SilentNotifier] and the app still works.
abstract class AppNotifier {
  /// Asks for the permission and prepares the channel. Safe to call twice.
  Future<void> initialize();

  /// A session finished its turn.
  Future<void> sessionSettled({
    required String sessionId,
    required String title,
    String? body,
  });

  /// A session's child died.
  Future<void> sessionFailed({
    required String sessionId,
    required String title,
    required String body,
  });
}

/// The real notifier: the OS notification centre.
class LocalNotifier implements AppNotifier {
  LocalNotifier({FlutterLocalNotificationsPlugin? plugin})
    : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  /// The Android channel every pi-ui notification lands on.
  static const channelId = 'piui.sessions';

  final FlutterLocalNotificationsPlugin _plugin;

  var _ready = false;

  @override
  Future<void> initialize() async {
    if (_ready) {
      return;
    }
    _ready = true;
    try {
      await _plugin.initialize(
        settings: const InitializationSettings(
          android: AndroidInitializationSettings('@mipmap/ic_launcher'),
          linux: LinuxInitializationSettings(defaultActionName: 'Open pi-ui'),
        ),
      );
      // Android 13+ asks the user; the other platforms either have no runtime
      // permission or grant it at install time.
      await _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >()
          ?.requestNotificationsPermission();
    } on Exception catch (error) {
      // A notification is a convenience: never let it break the session.
      debugPrint('pi-ui: notifications are off: $error');
      _ready = false;
    }
  }

  @override
  Future<void> sessionSettled({
    required String sessionId,
    required String title,
    String? body,
  }) => _show(
    id: notificationId(sessionId),
    title: title,
    body: body ?? 'The run finished.',
    payload: sessionId,
  );

  @override
  Future<void> sessionFailed({
    required String sessionId,
    required String title,
    required String body,
  }) => _show(
    id: notificationId(sessionId),
    title: title,
    body: body,
    payload: sessionId,
  );

  Future<void> _show({
    required int id,
    required String title,
    required String body,
    required String payload,
  }) async {
    if (!_ready) {
      await initialize();
    }
    try {
      await _plugin.show(
        id: id,
        title: title,
        body: body,
        payload: payload,
        notificationDetails: const NotificationDetails(
          android: AndroidNotificationDetails(
            channelId,
            'Sessions',
            channelDescription: 'A pi session finished or failed.',
            importance: Importance.defaultImportance,
            priority: Priority.defaultPriority,
          ),
          linux: LinuxNotificationDetails(),
        ),
      );
    } on Exception catch (error) {
      debugPrint('pi-ui: notification not shown: $error');
    }
  }

  /// A stable per-session id, so a newer notification replaces the older one.
  static int notificationId(String sessionId) =>
      sessionId.hashCode & 0x7fffffff;
}

/// A notifier for platforms and tests that need none.
class SilentNotifier implements AppNotifier {
  const SilentNotifier();

  @override
  Future<void> initialize() async {}

  @override
  Future<void> sessionSettled({
    required String sessionId,
    required String title,
    String? body,
  }) async {}

  @override
  Future<void> sessionFailed({
    required String sessionId,
    required String title,
    required String body,
  }) async {}
}

/// A notifier a test reads afterwards.
class RecordingNotifier implements AppNotifier {
  final List<({String sessionId, String title, String body})> shown = [];

  var initialized = 0;

  @override
  Future<void> initialize() async => initialized++;

  @override
  Future<void> sessionSettled({
    required String sessionId,
    required String title,
    String? body,
  }) async => shown.add((
    sessionId: sessionId,
    title: title,
    body: body ?? 'The run finished.',
  ));

  @override
  Future<void> sessionFailed({
    required String sessionId,
    required String title,
    required String body,
  }) async => shown.add((sessionId: sessionId, title: title, body: body));
}

/// Watches the event stream and notifies when a run ends while the app is in
/// the background.
///
/// Only two things are worth an interruption: a turn that finished (the answer
/// the user is waiting for) and a child that died. Everything else is already on
/// the timeline.
class SessionWatcher {
  SessionWatcher({
    required this.frames,
    required this.notifier,
    required this.isForeground,
    required this.titleOf,
  });

  /// The event stream to follow.
  final Stream<WsFrame> frames;

  /// Where a notification goes.
  final AppNotifier notifier;

  /// True while the app is on screen: an app in front of the user gets no toast.
  final bool Function() isForeground;

  /// The name of a session, for the notification title.
  final String Function(String sessionId) titleOf;

  StreamSubscription<WsFrame>? _subscription;

  /// Starts listening. Idempotent.
  void start() {
    _subscription ??= frames.listen(_onFrame, onError: (Object _) {});
  }

  Future<void> dispose() async {
    await _subscription?.cancel();
    _subscription = null;
  }

  void _onFrame(WsFrame frame) {
    if (isForeground() || frame is! WsEvent) {
      return;
    }
    final sessionId = frame.sessionId;
    if (sessionId == null) {
      return;
    }
    switch (frame.type) {
      case 'pi.agent_settled':
      case 'pi.agent_end':
        unawaited(
          notifier.sessionSettled(
            sessionId: sessionId,
            title: titleOf(sessionId),
          ),
        );
      case 'server.crashed':
        final code = frame.payload['exitCode'];
        unawaited(
          notifier.sessionFailed(
            sessionId: sessionId,
            title: titleOf(sessionId),
            body:
                'The child process died'
                '${code is num ? ' (exit $code)' : ''}.',
          ),
        );
    }
  }
}

/// Keeps the foreground state the watcher asks about.
///
/// A small object instead of a global: the app owns one, and a test can hand in
/// a constant.
class ForegroundState {
  ForegroundState({
    AppLifecycleListener? listener,
    void Function()? onResumed,
  }) {
    _listener =
        listener ??
        AppLifecycleListener(
          onStateChange: (state) {
            isForeground = state == AppLifecycleState.resumed;
            if (isForeground) {
              onResumed?.call();
            }
          },
        );
  }

  late final AppLifecycleListener _listener;

  /// True while the app is on screen. A test sets it directly.
  var isForeground = true;

  void dispose() => _listener.dispose();
}
