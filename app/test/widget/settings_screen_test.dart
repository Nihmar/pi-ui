import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/dto.dart';
import 'package:piui/core/api/profile.dart';
import 'package:piui/core/api/providers.dart';
import 'package:piui/core/api/settings.dart';
import 'package:piui/core/theme/app_theme.dart';
import 'package:piui/features/settings/settings_screen.dart';

/// A server with two settings, one changed from its default.
const _settings = ServerSettings(
  values: {'git.write': true, 'ui.theme': 'dark'},
  defaults: {'git.write': false, 'ui.theme': 'system'},
  known: [
    SettingDefinition(
      key: 'git.write',
      kind: 'bool',
      description: 'Allow git mutations.',
      defaultValue: false,
    ),
    SettingDefinition(
      key: 'ui.theme',
      kind: 'enum',
      description: 'Theme every client shows.',
      defaultValue: 'system',
    ),
  ],
);

/// Pumps the settings screen for one scope.
Future<void> pumpSettings(
  WidgetTester tester, {
  DeviceScope scope = DeviceScope.admin,
  UpdateReport report = const UpdateReport(managed: true),
}) async {
  tester.view.physicalSize = const Size(900, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        socketProvider.overrideWithValue(null),
        profileProvider.overrideWith(() => _FixedProfile(scope)),
        serverSettingsProvider.overrideWith((ref) async => _settings),
        updatesProvider.overrideWith((ref) async => report),
      ],
      child: MaterialApp(theme: AppTheme.dark(), home: const SettingsScreen()),
    ),
  );
  await tester.pumpAndSettle();
}

/// A profile controller that answers with one fixed device.
class _FixedProfile extends ProfileController {
  _FixedProfile(this.scope);

  final DeviceScope scope;

  @override
  Future<ServerProfile?> build() async => ServerProfile(
    baseUrl: 'http://pi-ui.test:8787',
    deviceName: 'test device',
    token: 'd_test.secret',
    deviceId: 'd_test',
    scope: scope,
  );
}

void main() {
  testWidgets('the device section names the server and the scope', (
    tester,
  ) async {
    await pumpSettings(tester);

    expect(find.text('This device'), findsOneWidget);
    expect(find.textContaining('http://pi-ui.test:8787'), findsOneWidget);
    expect(find.textContaining('test device · admin'), findsOneWidget);
  });

  testWidgets('the policy is rendered from the catalogue', (tester) async {
    await pumpSettings(tester);

    expect(find.text('git.write'), findsOneWidget);
    expect(find.text('Allow git mutations.'), findsOneWidget);
    // A bool is a switch, and it is on because the server says true.
    final toggle = tester.widget<Switch>(find.byType(Switch));
    expect(toggle.value, isTrue);
    // The two changed keys offer a way back to their default.
    expect(find.byTooltip('Back to the default'), findsNWidgets(2));
  });

  testWidgets('an operator reads the policy without switches', (tester) async {
    await pumpSettings(tester, scope: DeviceScope.operator);

    expect(find.text('git.write'), findsOneWidget);
    final toggle = tester.widget<Switch>(find.byType(Switch));
    expect(toggle.onChanged, isNull, reason: 'an operator cannot change it');
    expect(find.textContaining('needs an admin device'), findsOneWidget);
    // The update panel is admin-only and is not shown at all.
    expect(find.text('Updates'), findsNothing);
  });

  testWidgets(
    'the update panel hides the button when the deployment manages it',
    (tester) async {
      await pumpSettings(
        tester,
        report: const UpdateReport(
          managed: true,
          components: [
            UpdateComponent(
              name: 'pi',
              current: '0.87.1',
              latest: '0.91.0',
              updateAvailable: true,
            ),
          ],
        ),
      );

      expect(find.text('Updates'), findsOneWidget);
      expect(find.text('pi'), findsOneWidget);
      expect(find.textContaining('latest 0.91.0'), findsOneWidget);
      expect(find.text('Run the update'), findsNothing);
      expect(find.textContaining('manages its own updates'), findsOneWidget);
    },
  );

  testWidgets('an unmanaged deployment offers the update', (tester) async {
    await pumpSettings(
      tester,
      report: const UpdateReport(
        managed: false,
        components: [UpdateComponent(name: 'pi', current: '0.87.1')],
      ),
    );

    expect(find.text('Run the update'), findsOneWidget);
  });
}
