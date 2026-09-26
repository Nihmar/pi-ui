import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/providers.dart';

void main() {
  group('themeModeFrom', () {
    test('reads the two explicit choices', () {
      expect(themeModeFrom('dark'), ThemeMode.dark);
      expect(themeModeFrom('light'), ThemeMode.light);
    });

    test('anything else follows the system', () {
      expect(themeModeFrom('system'), ThemeMode.system);
      expect(themeModeFrom('sepia'), ThemeMode.system);
      expect(themeModeFrom(null), ThemeMode.system);
      expect(themeModeFrom(7), ThemeMode.system);
    });
  });
}
