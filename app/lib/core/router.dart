import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../features/files/file_browser_screen.dart';
import '../features/onboarding/connect_screen.dart';
import '../features/onboarding/pair_screen.dart';
import '../features/search/search_screen.dart';
import '../features/sessions/session_detail_screen.dart';
import '../features/sessions/session_list_screen.dart';
import '../features/settings/settings_screen.dart';
import '../features/shell/adaptive_shell.dart';
import '../features/splash_screen.dart';
import 'api/providers.dart';

/// The route names of the app, so a navigation call never spells a path twice.
abstract final class Routes {
  /// The splash that decides where a launch starts.
  static const splash = '/';

  /// The first screen of a fresh install.
  static const onboarding = '/onboarding';

  /// The pairing form, with the server it targets in the query string.
  static const pair = '/onboarding/pair';

  /// The session list (shell branch 0).
  static const sessions = '/sessions';

  /// The file browser (shell branch 1).
  static const files = '/files';

  /// Global search (shell branch 2).
  static const search = '/search';

  /// Settings (shell branch 3).
  static const settings = '/settings';

  /// The conversation of one session.
  static String chat(String sessionId) => '$sessions/$sessionId';

  /// One directory of a workspace.
  static String filesPath(String path) =>
      '$files?path=${Uri.encodeComponent(path)}';

  /// One file, opened in its own route (the phone layout).
  static String filePath(String path) =>
      '$files/view?path=${Uri.encodeComponent(path)}';

  /// The pairing form for one server.
  static String pairWith(String baseUrl) =>
      '$pair?url=${Uri.encodeComponent(baseUrl)}';
}

/// The router of the app.
///
/// The redirect is the onboarding gate: an unpaired client can only be on the
/// onboarding routes, a paired one never is. `StatefulShellRoute.indexedStack`
/// keeps one navigation stack per destination, which is what lets the desktop
/// rail and the phone bar share the branch state.
final routerProvider = Provider<GoRouter>((ref) {
  final refresh = ValueNotifier<int>(0);
  ref
    ..onDispose(refresh.dispose)
    ..listen(profileProvider, (_, _) => refresh.value++);
  return GoRouter(
    initialLocation: Routes.splash,
    refreshListenable: refresh,
    redirect: (context, state) {
      final profile = ref.read(profileProvider);
      final location = state.matchedLocation;
      if (location == Routes.splash && profile.isLoading) {
        return null;
      }
      final paired = profile.value?.isPaired ?? false;
      final atOnboarding = location.startsWith('/onboarding');
      if (!paired) {
        return atOnboarding ? null : Routes.onboarding;
      }
      if (atOnboarding || location == Routes.splash) {
        return Routes.sessions;
      }
      return null;
    },
    routes: [
      GoRoute(
        path: Routes.splash,
        builder: (context, state) => const SplashScreen(),
      ),
      GoRoute(
        path: Routes.onboarding,
        builder: (context, state) => const ConnectScreen(),
        routes: [
          GoRoute(
            path: 'pair',
            builder: (context, state) =>
                PairScreen(baseUrl: state.uri.queryParameters['url'] ?? ''),
          ),
        ],
      ),
      StatefulShellRoute.indexedStack(
        builder: (context, state, navigationShell) =>
            AdaptiveShell(navigationShell: navigationShell),
        branches: [
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.sessions,
                builder: (context, state) => const SessionListScreen(),
                routes: [
                  GoRoute(
                    path: ':id',
                    builder: (context, state) => SessionDetailScreen(
                      sessionId: state.pathParameters['id'] ?? '',
                    ),
                  ),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.files,
                builder: (context, state) => FileBrowserScreen(
                  initialPath: state.uri.queryParameters['path'],
                ),
                routes: [
                  GoRoute(
                    path: 'view',
                    builder: (context, state) => FileDetailScreen(
                      path: state.uri.queryParameters['path'] ?? '',
                    ),
                  ),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.search,
                builder: (context, state) => const SearchScreen(),
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.settings,
                builder: (context, state) => const SettingsScreen(),
              ),
            ],
          ),
        ],
      ),
      // A deep link to a session keeps working: the shell is the fallback.
      GoRoute(
        path: '/chat/:id',
        redirect: (context, state) =>
            Routes.chat(state.pathParameters['id'] ?? ''),
        builder: (context, state) => const SplashScreen(),
      ),
    ],
  );
});
