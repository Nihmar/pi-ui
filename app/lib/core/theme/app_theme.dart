import 'package:flutter/material.dart';

import 'theme_tokens.dart';

/// The two [ThemeData]s of pi-ui, both built from [AppTokens].
///
/// The mockup and the app share this file: the visual approval a reviewer gives
/// here is the approval of the colours, radii and typography the app will ship.
abstract final class AppTheme {
  /// The dark theme.
  static ThemeData dark() => _build(Brightness.dark, AppTokens.dark);

  /// The light theme.
  static ThemeData light() => _build(Brightness.light, AppTokens.light);

  static ThemeData _build(Brightness brightness, AppTokens tokens) {
    final scheme =
        ColorScheme.fromSeed(
          seedColor: tokens.accent,
          brightness: brightness,
        ).copyWith(
          surface: tokens.surface,
          onSurface: tokens.text,
          error: tokens.error,
          outline: tokens.border,
        );

    final base = brightness == Brightness.dark
        ? Typography.material2021().white
        : Typography.material2021().black;
    final textTheme = base
        .apply(bodyColor: tokens.text, displayColor: tokens.text)
        .copyWith(
          bodyMedium: base.bodyMedium?.copyWith(fontSize: 14.5, height: 1.45),
          bodySmall: base.bodySmall?.copyWith(color: tokens.textMuted),
          labelSmall: base.labelSmall?.copyWith(color: tokens.textDim),
        );

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      scaffoldBackgroundColor: tokens.background,
      textTheme: textTheme,
      extensions: [tokens],
      dividerColor: tokens.border,
      splashFactory: InkSparkle.splashFactory,
      appBarTheme: AppBarTheme(
        backgroundColor: tokens.background,
        foregroundColor: tokens.text,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: textTheme.titleMedium,
      ),
      cardTheme: CardThemeData(
        color: tokens.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(tokens.radiusMd),
          side: BorderSide(color: tokens.border),
        ),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: tokens.surface,
        indicatorColor: tokens.selectedBg,
        surfaceTintColor: Colors.transparent,
        height: 64,
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: tokens.surface,
        indicatorColor: tokens.selectedBg,
        selectedIconTheme: IconThemeData(color: tokens.accent),
        unselectedIconTheme: IconThemeData(color: tokens.textMuted),
        selectedLabelTextStyle: textTheme.labelMedium?.copyWith(
          color: tokens.accent,
        ),
        unselectedLabelTextStyle: textTheme.labelMedium?.copyWith(
          color: tokens.textMuted,
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: tokens.surfaceAlt,
        contentPadding: EdgeInsets.symmetric(
          horizontal: tokens.spaceMd,
          vertical: tokens.spaceMd,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(tokens.radiusMd),
          borderSide: BorderSide(color: tokens.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(tokens.radiusMd),
          borderSide: BorderSide(color: tokens.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(tokens.radiusMd),
          borderSide: BorderSide(color: tokens.accent, width: 1.5),
        ),
        hintStyle: TextStyle(color: tokens.textDim),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: tokens.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(tokens.radiusLg),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: tokens.surfaceAlt,
        contentTextStyle: textTheme.bodyMedium,
        behavior: SnackBarBehavior.floating,
      ),
      chipTheme: ChipThemeData(
        backgroundColor: tokens.surfaceAlt,
        side: BorderSide(color: tokens.border),
        labelStyle: textTheme.labelMedium,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(tokens.radiusSm),
        ),
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(
          color: tokens.surfaceAlt,
          borderRadius: BorderRadius.circular(tokens.radiusSm),
          border: Border.all(color: tokens.border),
        ),
        textStyle: textTheme.bodySmall,
      ),
    );
  }
}
