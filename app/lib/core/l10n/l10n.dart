import 'package:flutter/widgets.dart';

import '../../l10n/app_localizations.dart';

/// The app's own text, from the generated `AppLocalizations`.
///
/// `context.l10n` is the only way a widget gets a string: a literal in a widget is a string
/// nobody can translate, and `flutter analyze` cannot see the difference — so the rule is a
/// review one, backed by the ARB files being the single home of every user-facing sentence.
extension L10nContext on BuildContext {
  /// The localizations in scope. The app always provides them; a widget test must pass
  /// `testApp()` (test/support) or the delegates, or this throws — which is the loud
  /// failure a missing delegate deserves.
  AppLocalizations get l10n => AppLocalizations.of(this);
}

/// The locale one `ui.language` value asks for, or null when the client should follow the
/// device.
///
/// The server stores a language tag (`en`, `it`); a value this build does not ship falls back
/// to the device, which is better than an English screen for somebody whose phone is Italian
/// and whose admin set an unknown language.
Locale? localeForLanguage(Object? language, List<Locale> supported) {
  if (language is! String || language.trim().isEmpty) {
    return null;
  }
  // A tag may carry a region (`it-IT`, `en_GB`): the translation is per language, and
  // `Locale('it-IT')` would keep the whole string as its language code, which matches
  // nothing.
  final code = language.trim().toLowerCase().split(RegExp('[-_]')).first;
  if (code.isEmpty) {
    return null;
  }
  for (final candidate in supported) {
    if (candidate.languageCode == code) {
      return candidate;
    }
  }
  return null;
}
