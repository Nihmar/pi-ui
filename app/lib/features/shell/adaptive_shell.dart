import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

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

  static const _destinations = <_Destination>[
    _Destination(
      label: 'Sessions',
      icon: Icons.forum_outlined,
      selectedIcon: Icons.forum,
    ),
    _Destination(
      label: 'Search',
      icon: Icons.search_outlined,
      selectedIcon: Icons.search,
    ),
    _Destination(
      label: 'Settings',
      icon: Icons.settings_outlined,
      selectedIcon: Icons.settings,
    ),
  ];

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
                for (final destination in _destinations)
                  NavigationRailDestination(
                    icon: Icon(destination.icon),
                    selectedIcon: Icon(destination.selectedIcon),
                    label: Text(destination.label),
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
          for (final destination in _destinations)
            NavigationDestination(
              icon: Icon(destination.icon),
              selectedIcon: Icon(destination.selectedIcon),
              label: destination.label,
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
  const _Destination({
    required this.label,
    required this.icon,
    required this.selectedIcon,
  });

  final String label;
  final IconData icon;
  final IconData selectedIcon;
}
