import 'package:flutter/widgets.dart';

/// The layout breakpoints of pi-ui (PLAN.md §7).
///
/// * [compact] — below 600 logic pixels: a phone. NavigationBar, full-screen
///   routes, drawers instead of panes.
/// * [expanded] — 1024 and above: a desktop. NavigationRail, master/detail,
///   resizable panes, shortcuts and hover.
/// * everything between is the tablet/laptop band: one pane plus a rail.
abstract final class Breakpoints {
  /// Below this width the layout is a phone layout.
  static const double compact = 600;

  /// At or above this width the layout is a desktop layout.
  static const double expanded = 1024;
}

/// The breakpoint the current window falls into.
enum LayoutSize {
  /// A phone: one thing at a time.
  compact,

  /// A tablet or a small window: one pane, a rail or a drawer.
  medium,

  /// A desktop: panes side by side.
  expanded;

  /// The layout a window of [width] pixels gets.
  static LayoutSize of(double width) {
    if (width < Breakpoints.compact) {
      return LayoutSize.compact;
    }
    if (width >= Breakpoints.expanded) {
      return LayoutSize.expanded;
    }
    return LayoutSize.medium;
  }
}

/// Convenience accessors on the current [MediaQuery].
extension LayoutSizeContext on BuildContext {
  LayoutSize get layoutSize => LayoutSize.of(MediaQuery.sizeOf(this).width);

  /// True for the phone layout.
  bool get isCompact => layoutSize == LayoutSize.compact;

  /// True for the desktop layout.
  bool get isExpanded => layoutSize == LayoutSize.expanded;

  /// True for the in-between band.
  bool get isMedium => layoutSize == LayoutSize.medium;
}
