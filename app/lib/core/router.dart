import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../features/search/search_screen.dart';
import '../features/sessions/session_list_screen.dart';
import '../features/settings/settings_screen.dart';
import '../features/shell/adaptive_shell.dart';

/// The route names of the app, so a navigation call never spells a path twice.
abstract final class Routes {
  /// The session list (shell branch 0).
  static const sessions = '/sessions';

  /// Global search (shell branch 1).
  static const search = '/search';

  /// Settings (shell branch 2).
  static const settings = '/settings';
}

/// The router of the app. `StatefulShellRoute.indexedStack` keeps one navigation
/// stack per destination, which is what lets the desktop rail and the phone bar
/// share the branch state.
final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: Routes.sessions,
    routes: [
      StatefulShellRoute.indexedStack(
        builder: (context, state, navigationShell) =>
            AdaptiveShell(navigationShell: navigationShell),
        branches: [
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.sessions,
                builder: (context, state) => const SessionListScreen(),
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
    ],
  );
});
