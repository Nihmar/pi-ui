import 'package:flutter/material.dart';

/// The design tokens of pi-ui.
///
/// Every colour, radius and spacing a widget uses comes from here through
/// `Theme.of(context).extension<AppTokens>()!`; a widget never hard-codes one.
/// The mockup and the app share this file, so approving a mockup approves the
/// widget it will become.
@immutable
class AppTokens extends ThemeExtension<AppTokens> {
  const AppTokens({
    required this.accent,
    required this.background,
    required this.surface,
    required this.surfaceAlt,
    required this.border,
    required this.text,
    required this.textMuted,
    required this.textDim,
    required this.success,
    required this.error,
    required this.warning,
    required this.userBubble,
    required this.assistantBubble,
    required this.toolPendingBg,
    required this.toolSuccessBg,
    required this.toolErrorBg,
    required this.thinking,
    required this.selectedBg,
    required this.searchMatchBg,
    required this.codeBg,
    required this.codeBorder,
    this.spaceXs = 4,
    this.spaceSm = 8,
    this.spaceMd = 12,
    this.spaceLg = 16,
    this.spaceXl = 24,
    this.radiusSm = 6,
    this.radiusMd = 10,
    this.radiusLg = 16,
  });

  // Colours.
  final Color accent;
  final Color background;
  final Color surface;
  final Color surfaceAlt;
  final Color border;
  final Color text;
  final Color textMuted;
  final Color textDim;
  final Color success;
  final Color error;
  final Color warning;
  final Color userBubble;
  final Color assistantBubble;
  final Color toolPendingBg;
  final Color toolSuccessBg;
  final Color toolErrorBg;
  final Color thinking;
  final Color selectedBg;
  final Color searchMatchBg;
  final Color codeBg;
  final Color codeBorder;

  // Spacing scale.
  final double spaceXs;
  final double spaceSm;
  final double spaceMd;
  final double spaceLg;
  final double spaceXl;

  // Radii.
  final double radiusSm;
  final double radiusMd;
  final double radiusLg;

  /// The dark palette: the default of a coding session at night.
  static const AppTokens dark = AppTokens(
    accent: Color(0xFF4C8DFF),
    background: Color(0xFF0F1115),
    surface: Color(0xFF161A20),
    surfaceAlt: Color(0xFF1D2229),
    border: Color(0xFF2A3038),
    text: Color(0xFFE6E9EF),
    textMuted: Color(0xFF9AA3B2),
    textDim: Color(0xFF6B7482),
    success: Color(0xFF3FB950),
    error: Color(0xFFF85149),
    warning: Color(0xFFD29922),
    userBubble: Color(0xFF22354F),
    assistantBubble: Color(0xFF161A20),
    toolPendingBg: Color(0xFF1D2229),
    toolSuccessBg: Color(0xFF14251A),
    toolErrorBg: Color(0xFF2B1618),
    thinking: Color(0xFF8B93A3),
    selectedBg: Color(0xFF2B3442),
    searchMatchBg: Color(0xFF6B5E1F),
    codeBg: Color(0xFF11141A),
    codeBorder: Color(0xFF262C34),
  );

  /// The light palette.
  static const AppTokens light = AppTokens(
    accent: Color(0xFF2563EB),
    background: Color(0xFFF7F8FA),
    surface: Color(0xFFFFFFFF),
    surfaceAlt: Color(0xFFF0F2F5),
    border: Color(0xFFDDE1E6),
    text: Color(0xFF1B1F24),
    textMuted: Color(0xFF5A6472),
    textDim: Color(0xFF8A93A0),
    success: Color(0xFF1A7F37),
    error: Color(0xFFCF222E),
    warning: Color(0xFF9A6700),
    userBubble: Color(0xFFDBEAFE),
    assistantBubble: Color(0xFFFFFFFF),
    toolPendingBg: Color(0xFFF0F2F5),
    toolSuccessBg: Color(0xFFE6F4EA),
    toolErrorBg: Color(0xFFFDECEC),
    thinking: Color(0xFF6E7781),
    selectedBg: Color(0xFFDDE7F5),
    searchMatchBg: Color(0xFFFFF3B0),
    codeBg: Color(0xFFF6F8FA),
    codeBorder: Color(0xFFE2E6EB),
  );

  @override
  AppTokens copyWith({
    Color? accent,
    Color? background,
    Color? surface,
    Color? surfaceAlt,
    Color? border,
    Color? text,
    Color? textMuted,
    Color? textDim,
    Color? success,
    Color? error,
    Color? warning,
    Color? userBubble,
    Color? assistantBubble,
    Color? toolPendingBg,
    Color? toolSuccessBg,
    Color? toolErrorBg,
    Color? thinking,
    Color? selectedBg,
    Color? searchMatchBg,
    Color? codeBg,
    Color? codeBorder,
    double? spaceXs,
    double? spaceSm,
    double? spaceMd,
    double? spaceLg,
    double? spaceXl,
    double? radiusSm,
    double? radiusMd,
    double? radiusLg,
  }) {
    return AppTokens(
      accent: accent ?? this.accent,
      background: background ?? this.background,
      surface: surface ?? this.surface,
      surfaceAlt: surfaceAlt ?? this.surfaceAlt,
      border: border ?? this.border,
      text: text ?? this.text,
      textMuted: textMuted ?? this.textMuted,
      textDim: textDim ?? this.textDim,
      success: success ?? this.success,
      error: error ?? this.error,
      warning: warning ?? this.warning,
      userBubble: userBubble ?? this.userBubble,
      assistantBubble: assistantBubble ?? this.assistantBubble,
      toolPendingBg: toolPendingBg ?? this.toolPendingBg,
      toolSuccessBg: toolSuccessBg ?? this.toolSuccessBg,
      toolErrorBg: toolErrorBg ?? this.toolErrorBg,
      thinking: thinking ?? this.thinking,
      selectedBg: selectedBg ?? this.selectedBg,
      searchMatchBg: searchMatchBg ?? this.searchMatchBg,
      codeBg: codeBg ?? this.codeBg,
      codeBorder: codeBorder ?? this.codeBorder,
      spaceXs: spaceXs ?? this.spaceXs,
      spaceSm: spaceSm ?? this.spaceSm,
      spaceMd: spaceMd ?? this.spaceMd,
      spaceLg: spaceLg ?? this.spaceLg,
      spaceXl: spaceXl ?? this.spaceXl,
      radiusSm: radiusSm ?? this.radiusSm,
      radiusMd: radiusMd ?? this.radiusMd,
      radiusLg: radiusLg ?? this.radiusLg,
    );
  }

  @override
  AppTokens lerp(covariant AppTokens? other, double t) {
    if (other == null) {
      return this;
    }
    Color mix(Color a, Color b) => Color.lerp(a, b, t)!;
    double blend(double a, double b) => a + (b - a) * t;
    return AppTokens(
      accent: mix(accent, other.accent),
      background: mix(background, other.background),
      surface: mix(surface, other.surface),
      surfaceAlt: mix(surfaceAlt, other.surfaceAlt),
      border: mix(border, other.border),
      text: mix(text, other.text),
      textMuted: mix(textMuted, other.textMuted),
      textDim: mix(textDim, other.textDim),
      success: mix(success, other.success),
      error: mix(error, other.error),
      warning: mix(warning, other.warning),
      userBubble: mix(userBubble, other.userBubble),
      assistantBubble: mix(assistantBubble, other.assistantBubble),
      toolPendingBg: mix(toolPendingBg, other.toolPendingBg),
      toolSuccessBg: mix(toolSuccessBg, other.toolSuccessBg),
      toolErrorBg: mix(toolErrorBg, other.toolErrorBg),
      thinking: mix(thinking, other.thinking),
      selectedBg: mix(selectedBg, other.selectedBg),
      searchMatchBg: mix(searchMatchBg, other.searchMatchBg),
      codeBg: mix(codeBg, other.codeBg),
      codeBorder: mix(codeBorder, other.codeBorder),
      spaceXs: blend(spaceXs, other.spaceXs),
      spaceSm: blend(spaceSm, other.spaceSm),
      spaceMd: blend(spaceMd, other.spaceMd),
      spaceLg: blend(spaceLg, other.spaceLg),
      spaceXl: blend(spaceXl, other.spaceXl),
      radiusSm: blend(radiusSm, other.radiusSm),
      radiusMd: blend(radiusMd, other.radiusMd),
      radiusLg: blend(radiusLg, other.radiusLg),
    );
  }
}

/// Reads the tokens of the current theme. A widget that needs a colour, a gap or
/// a radius calls this instead of a literal.
extension AppTokensContext on BuildContext {
  AppTokens get tokens => Theme.of(this).extension<AppTokens>()!;
}
