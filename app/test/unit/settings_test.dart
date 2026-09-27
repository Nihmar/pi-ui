import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/settings.dart';

void main() {
  group('ServerSettings', () {
    test('reads the values, the defaults and the catalogue', () {
      final settings = ServerSettings.fromJson({
        'values': {'git.write': true, 'ui.theme': 'dark'},
        'defaults': {'git.write': false, 'ui.theme': 'system'},
        'known': [
          {
            'key': 'git.write',
            'kind': 'bool',
            'default': false,
            'description': 'Allow git mutations.',
          },
          {
            'key': 'ui.theme',
            'kind': 'enum',
            'default': 'system',
            'description': 'Theme every client shows.',
          },
        ],
      });

      expect(settings.values['git.write'], isTrue);
      expect(settings.valueOf('git.write'), isTrue);
      expect(settings.valueOf('missing'), isNull);
      expect(settings.defaults['ui.theme'], 'system');
      expect(settings.known, hasLength(2));
      expect(settings.known.first.kind, 'bool');
      expect(settings.known.first.description, isNotEmpty);
      expect(settings.isDefault('ui.theme'), isFalse);
      expect(settings.isDefault('git.write'), isFalse);
      expect(settings.isEmpty, isFalse);
    });

    test(
      'an unconfigured server answers nothing, and that is not an error',
      () {
        final settings = ServerSettings.fromJson(null);
        expect(settings.isEmpty, isTrue);
        expect(settings.known, isEmpty);
        expect(settings.valueOf('ui.theme'), isNull);
      },
    );

    test('a value equal to its default counts as unchanged', () {
      final settings = ServerSettings.fromJson({
        'values': {'ui.theme': 'system'},
        'defaults': {'ui.theme': 'system'},
        'known': [
          {
            'key': 'ui.theme',
            'kind': 'enum',
            'default': 'system',
            'description': 'x',
          },
        ],
      });
      expect(settings.isDefault('ui.theme'), isTrue);
    });
  });

  group('UpdateReport', () {
    test('reads the components and the managed flag', () {
      final report = UpdateReport.fromJson({
        'managed': false,
        'components': [
          {
            'name': 'pi',
            'current': '0.87.1',
            'latest': '0.91.0',
            'updateAvailable': true,
            'checkedAt': '2026-09-26T12:00:00Z',
          },
          {
            'name': 'server',
            'current': '1.0.0',
            'error': 'the check timed out',
          },
        ],
      });

      expect(report.managed, isFalse);
      expect(report.hasUpdates, isTrue);
      expect(report.components.first.latest, '0.91.0');
      expect(report.components.first.checkedAt, isNotNull);
      expect(report.components.last.isUnknown, isTrue);
      expect(report.components.last.updateAvailable, isFalse);
    });

    test('an absent managed flag reads as managed', () {
      // The safe default: a client that assumes it may apply updates would offer a button
      // that answers 409.
      final report = UpdateReport.fromJson(null);
      expect(report.components, isEmpty);
      expect(report.managed, isTrue);
      expect(report.hasUpdates, isFalse);
    });
  });
}
