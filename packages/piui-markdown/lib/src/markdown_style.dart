import 'package:flutter/widgets.dart';

/// The visual contract of the markdown engine.
///
/// The app builds one of these from its own theme tokens and passes it to
/// [MarkdownView]; the engine never reads a hard-coded colour from the
/// application. [MarkdownStyle.fallback] exists for tests and for a caller that
/// has no theme at all.
@immutable
class MarkdownStyle {
  const MarkdownStyle({
    required this.bodyStyle,
    required this.headingStyles,
    required this.codeStyle,
    required this.inlineCodeStyle,
    required this.linkStyle,
    required this.inlineCodeBackground,
    required this.codeBackground,
    required this.codeBorderColor,
    required this.codeLabelStyle,
    required this.quoteBorderColor,
    required this.ruleColor,
    this.blockSpacing = 12,
    this.listIndent = 24,
    this.codePadding = const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
    this.radius = 8,
  }) : assert(headingStyles.length == 6, 'one style per heading level');

  /// The base style of prose.
  final TextStyle bodyStyle;

  /// One style per heading level, index 0 being level 1.
  final List<TextStyle> headingStyles;

  /// The monospace style of code blocks and inline code.
  final TextStyle codeStyle;

  /// The style of an inline code span, on top of [codeStyle].
  final TextStyle inlineCodeStyle;

  /// The style of a link label.
  final TextStyle linkStyle;

  /// The background of an inline code span.
  final Color inlineCodeBackground;

  /// The background of a fenced code block.
  final Color codeBackground;

  /// The border of a fenced code block.
  final Color codeBorderColor;

  /// The style of the fence language label.
  final TextStyle codeLabelStyle;

  /// The left border of a block quote.
  final Color quoteBorderColor;

  /// The colour of a thematic break.
  final Color ruleColor;

  /// Vertical space between two blocks.
  final double blockSpacing;

  /// Indentation of a list item's content.
  final double listIndent;

  /// Padding inside a fenced code block.
  final EdgeInsetsGeometry codePadding;

  /// Corner radius of the code and quote surfaces.
  final double radius;

  /// A neutral style that reads under any theme, used by tests and previews.
  factory MarkdownStyle.fallback() {
    const code = TextStyle(fontFamily: 'monospace', fontSize: 13, height: 1.45);
    const body = TextStyle(fontSize: 14, height: 1.5);
    return MarkdownStyle(
      bodyStyle: body,
      headingStyles: List<TextStyle>.generate(
        6,
        (index) => body.copyWith(
          fontSize: 22 - index * 2,
          fontWeight: FontWeight.w600,
          height: 1.3,
        ),
      ),
      codeStyle: code,
      inlineCodeStyle: code.copyWith(fontSize: 13),
      linkStyle: body.copyWith(
        color: const Color(0xFF3B82F6),
        decoration: TextDecoration.underline,
      ),
      inlineCodeBackground: const Color(0x14000000),
      codeBackground: const Color(0x0D000000),
      codeBorderColor: const Color(0x1A000000),
      codeLabelStyle: body.copyWith(
        fontSize: 11,
        color: const Color(0x99000000),
        letterSpacing: 0.4,
      ),
      quoteBorderColor: const Color(0x33000000),
      ruleColor: const Color(0x1A000000),
    );
  }
}
