import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'router.dart';
import 'theme/app_theme.dart';

/// The root of the mockup.
///
/// It uses the same router, themes and tokens the real app will: approving this
/// app is approving the widget tree of `app/`, with `MockPiApi` in place of the
/// network.
class PiuiMockApp extends ConsumerWidget {
  const PiuiMockApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);
    return MaterialApp.router(
      title: 'pi-ui mockup',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: ThemeMode.system,
      routerConfig: router,
    );
  }
}
