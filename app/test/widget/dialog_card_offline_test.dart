import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/fake_channel.dart';
import '../support/test_app.dart';

import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/api/socket.dart';
import 'package:piui/core/models/chat_entry.dart';
import 'package:piui/features/chat/widgets/dialog_card.dart';

const _profile = ServerProfile(
  baseUrl: 'http://pi-ui.test:8787',
  deviceName: 'test',
  token: 'd_test.secret',
);

/// A socket whose handshake never completes: the state the client is in while the server
/// is unreachable.
PiUiSocket offlineSocket() => PiUiSocket(
  profile: _profile,
  channels: (uri, headers) => FakeChannel(failReady: true),
  handshakeTimeout: const Duration(milliseconds: 40),
  baseBackoff: const Duration(milliseconds: 5),
  maxBackoff: const Duration(milliseconds: 10),
  jitter: false,
);

void main() {
  testWidgets('an approval tapped while offline is reported, not lost', (
    tester,
  ) async {
    final socket = offlineSocket();
    addTearDown(socket.dispose);
    var answered = false;

    await tester.pumpWidget(
      ProviderScope(
        overrides: [socketProvider.overrideWithValue(socket)],
        child: testApp(
          home: Scaffold(
            body: DialogCard(
              sessionId: 's_1',
              request: DialogRequest(
                id: 'r_1',
                sessionId: 's_1',
                method: DialogMethod.confirm,
                title: 'pi-ui-bridge: confirm command',
                message: 'rm -rf /srv/app',
                at: DateTime.now(),
                expiresAt: DateTime.now().add(const Duration(minutes: 5)),
              ),
              onAnswered: (_) => answered = true,
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Approve'));
    await tester.pump();

    // The answer never reached the server, so the card must not claim it did: the user is
    // told why and can try again once the connection is back.
    expect(answered, isFalse, reason: 'the answer never reached the server');
    expect(find.byType(SnackBar), findsOneWidget);
    expect(tester.takeException(), isNull);

    // The card is disposed before the test ends: its countdown is a periodic timer.
    await tester.pumpWidget(const SizedBox());
  });
}
