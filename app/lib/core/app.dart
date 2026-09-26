import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api/providers.dart';
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
    return MaterialApp.router(
      title: 'pi-ui',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: ThemeMode.system,
      routerConfig: router,
    );
  }
}
