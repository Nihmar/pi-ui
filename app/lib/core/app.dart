import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../l10n/app_localizations.dart';
import 'api/providers.dart';
import 'l10n/l10n.dart';
import 'router.dart';
import 'theme/app_theme.dart';

/// The root of the pi-ui client.
class PiuiApp extends ConsumerWidget {
  const PiuiApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);
    // The watcher and the lifecycle listener exist for the app, not for a screen:
    // a session the user never opened still notifies when it is done.
    ref
      ..watch(foregroundProvider)
      ..watch(sessionWatcherProvider);
    // The theme and the language are the deployment's choice (`ui.theme`, `ui.language`),
    // not per-device settings: a server meant to look and read a certain way says so once.
    final themeMode = ref.watch(themeModeProvider);
    final locale = localeForLanguage(
      ref.watch(serverSettingsProvider).value?.values['ui.language'],
      AppLocalizations.supportedLocales,
    );
    return MaterialApp.router(
      onGenerateTitle: (context) => context.l10n.appTitle,
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: themeMode,
      locale: locale,
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      routerConfig: router,
    );
  }
}
