import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/l10n/l10n.dart';
import '../../core/theme/breakpoints.dart';
import '../../core/theme/theme_tokens.dart';

/// The adaptive shell of pi-ui: a NavigationBar on a phone, a NavigationRail from
/// the desktop breakpoint up, the same branch state in both.
///
/// It wraps the `StatefulShellRoute.indexedStack` of the router, so each
/// destination keeps its own navigation stack.
class AdaptiveShell extends StatelessWidget {
  const AdaptiveShell({super.key, required this.navigationShell});

  /// The branch container the router hands the shell.
  final StatefulNavigationShell navigationShell;

  /// The branch a destination opens, in the order the router declares them.
  static const _destinations = <_Destination>[
    _Destination(icon: Icons.forum_outlined, selectedIcon: Icons.forum),
    _Destination(icon: Icons.folder_outlined, selectedIcon: Icons.folder),
    _Destination(icon: Icons.search_outlined, selectedIcon: Icons.search),
    _Destination(icon: Icons.settings_outlined, selectedIcon: Icons.settings),
  ];

  /// The label of one destination: translated where it is rendered, because a `const` list
  /// cannot hold a string that depends on the locale.
  static String labelOf(BuildContext context, int index) => switch (index) {
    0 => context.l10n.navSessions,
    1 => context.l10n.navFiles,
    2 => context.l10n.navSearch,
    _ => context.l10n.navSettings,
  };

  void _go(int index) {
    navigationShell.goBranch(
      index,
      initialLocation: index == navigationShell.currentIndex,
    );
  }

  @override
  Widget build(BuildContext context) {
    if (context.isExpanded) {
      return Scaffold(
        body: Row(
          children: [
            NavigationRail(
              selectedIndex: navigationShell.currentIndex,
              onDestinationSelected: _go,
              labelType: NavigationRailLabelType.all,
              leading: Padding(
                padding: EdgeInsets.symmetric(vertical: context.tokens.spaceMd),
                child: _Brand(),
              ),
              destinations: [
                for (var index = 0; index < _destinations.length; index++)
                  NavigationRailDestination(
                    icon: Icon(_destinations[index].icon),
                    selectedIcon: Icon(_destinations[index].selectedIcon),
                    label: Text(labelOf(context, index)),
                  ),
              ],
            ),
            const VerticalDivider(width: 1),
            Expanded(child: navigationShell),
          ],
        ),
      );
    }

    return Scaffold(
      body: navigationShell,
      bottomNavigationBar: NavigationBar(
        selectedIndex: navigationShell.currentIndex,
        onDestinationSelected: _go,
        destinations: [
          for (var index = 0; index < _destinations.length; index++)
            NavigationDestination(
              icon: Icon(_destinations[index].icon),
              selectedIcon: Icon(_destinations[index].selectedIcon),
              label: labelOf(context, index),
            ),
        ],
      ),
    );
  }
}

/// The little product mark above the rail.
class _Brand extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    return Tooltip(
      message: 'pi-ui mockup',
      child: Container(
        width: 40,
        height: 40,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: tokens.accent,
          borderRadius: BorderRadius.circular(tokens.radiusMd),
        ),
        child: const Text(
          'π',
          style: TextStyle(
            color: Colors.white,
            fontSize: 20,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
    );
  }
}

/// One shell destination.
class _Destination {
  const _Destination({required this.icon, required this.selectedIcon});

  final IconData icon;
  final IconData selectedIcon;
}
