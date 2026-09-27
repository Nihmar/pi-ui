import 'package:flutter/material.dart';
import 'package:piui/core/theme/app_theme.dart';
import 'package:piui/l10n/app_localizations.dart';

/// The `MaterialApp` a widget test pumps a screen in.
///
/// The delegates are the point: `context.l10n` throws without them, and a test that forgets
/// them would fail on a localization error instead of on what it asserts. English is pinned,
/// so an assertion on a sentence is not a bet on the test machine's locale.
MaterialApp testApp({
  required Widget home,
  ThemeData? theme,
  Locale locale = const Locale('en'),
}) => MaterialApp(
  theme: theme ?? AppTheme.dark(),
  locale: locale,
  localizationsDelegates: AppLocalizations.localizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: home,
);
