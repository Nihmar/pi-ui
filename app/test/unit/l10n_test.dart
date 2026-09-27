import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/l10n/l10n.dart';
import 'package:piui/l10n/app_localizations.dart';

void main() {
  group('localeForLanguage', () {
    test('follows the device when the server says nothing', () {
      expect(
        localeForLanguage(null, AppLocalizations.supportedLocales),
        isNull,
      );
      expect(localeForLanguage('', AppLocalizations.supportedLocales), isNull);
      expect(localeForLanguage(7, AppLocalizations.supportedLocales), isNull);
    });

    test('reads a language tag the build ships', () {
      expect(
        localeForLanguage('it', AppLocalizations.supportedLocales),
        const Locale('it'),
      );
      // A region is the same language: `en-GB` is served by the `en` translation.
      expect(
        localeForLanguage('en-GB', AppLocalizations.supportedLocales),
        const Locale('en'),
      );
    });

    test(
      'falls back to the device for a language this build does not ship',
      () {
        // An English screen for an Italian phone is worse than the device's own choice: the
        // server asked for something this build cannot do, so the app does what it knows.
        expect(
          localeForLanguage('de', AppLocalizations.supportedLocales),
          isNull,
        );
      },
    );
  });

  group('the generated translations', () {
    test('English is the source language', () {
      final en = lookupAppLocalizations(const Locale('en'));
      expect(en.navSessions, 'Sessions');
      expect(en.searchNoMatchTitle, 'No match');
    });

    test('Italian is a translation, not a copy', () {
      final en = lookupAppLocalizations(const Locale('en'));
      final it = lookupAppLocalizations(const Locale('it'));

      // A handful of keys across the app: this is what catches an ARB that was copied and
      // never translated.
      expect(it.navSessions, isNot(en.navSessions));
      expect(it.noSessionsTitle, isNot(en.noSessionsTitle));
      expect(it.composerSend, isNot(en.composerSend));
      expect(it.gitCommit, isNot(en.gitCommit));
      expect(it.settingsPolicySection, isNot(en.settingsPolicySection));
      expect(it.terminalReady, isNot(en.terminalReady));
      expect(it.pairSubmit, isNot(en.pairSubmit));

      // A placeholder survives translation.
      expect(it.messagesCount('3'), contains('3'));
      expect(it.updatesChecked('oggi'), contains('oggi'));
    });
  });
}
