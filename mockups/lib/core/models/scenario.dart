import 'package:flutter/material.dart';

/// The scenarios the mockup can replay without a server.
///
/// The recorder in the settings/debug drawer lists them; each one drives the same
/// `MockPiApi` streams the real client consumes, so a screen is reviewed against
/// the event shapes the server really sends.
enum MockScenario {
  streaming(
    label: 'Streaming answer',
    description:
        'A user prompt, tokens arriving, a thinking block, then the end.',
    icon: Icons.stream,
  ),
  toolCall(
    label: 'Tool call with diff',
    description: 'A bash call and an edit call, with output and a file diff.',
    icon: Icons.terminal,
  ),
  extensionDialog(
    label: 'Approval dialog',
    description: 'A blocking confirm with a countdown, then the answer.',
    icon: Icons.help_outline,
  ),
  dialogTimeout(
    label: 'Dialog timeout',
    description: 'A dialog nobody answers, cancelled by the server.',
    icon: Icons.timer_off_outlined,
  ),
  providerError(
    label: 'Provider quota exhausted',
    description: 'The model provider rejects the turn with a coded error.',
    icon: Icons.error_outline,
  ),
  sessionCrash(
    label: 'Session crash',
    description:
        'The child dies with an exit code and the session turns crashed.',
    icon: Icons.warning_amber_outlined,
  ),
  offlineQueue(
    label: 'Offline queue',
    description:
        'Messages queued while offline, sent when the connection returns.',
    icon: Icons.cloud_off_outlined,
  ),
  reconnection(
    label: 'Reconnect with replay',
    description:
        'A dropped socket, the replay of missed events, then live again.',
    icon: Icons.sync,
  ),
  wrapUp(
    label: 'Wrap-up and handoff',
    description: 'A session closing gracefully with a handoff note saved.',
    icon: Icons.archive_outlined,
  );

  const MockScenario({
    required this.label,
    required this.description,
    required this.icon,
  });

  /// The label of the recorder entry.
  final String label;

  /// The one-line explanation of the recorder entry.
  final String description;

  /// The recorder entry's glyph.
  final IconData icon;
}

/// The connection state the status banner renders.
enum MockConnection {
  online,
  offline,
  reconnecting;

  /// The word the banner shows.
  String get label => switch (this) {
    MockConnection.online => 'Connected',
    MockConnection.offline => 'Offline',
    MockConnection.reconnecting => 'Reconnecting…',
  };
}
