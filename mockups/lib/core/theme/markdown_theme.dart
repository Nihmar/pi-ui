import 'package:flutter/material.dart';
import 'package:piui_markdown/piui_markdown.dart';

import 'theme_tokens.dart';

/// Maps the pi-ui tokens onto the markdown engine's visual contract.
///
/// The engine never reads an application colour: it renders [MarkdownStyle], and
/// this is the one place where a pi-ui theme produces one.
MarkdownStyle markdownStyleFor(AppTokens tokens) {
  final body = TextStyle(fontSize: 14.5, height: 1.5, color: tokens.text);
  return MarkdownStyle(
    bodyStyle: body,
    headingStyles: List<TextStyle>.generate(
      6,
      (index) => body.copyWith(
        fontSize: 21 - index * 2,
        fontWeight: FontWeight.w600,
        height: 1.3,
      ),
    ),
    codeStyle: TextStyle(
      fontFamily: 'monospace',
      fontSize: 13,
      height: 1.45,
      color: tokens.text,
    ),
    inlineCodeStyle: TextStyle(
      fontFamily: 'monospace',
      fontSize: 13,
      color: tokens.text,
    ),
    inlineCodeBackground: tokens.surfaceAlt,
    codeBackground: tokens.codeBg,
    codeBorderColor: tokens.codeBorder,
    codeLabelStyle: TextStyle(
      fontSize: 11,
      color: tokens.textDim,
      letterSpacing: 0.4,
    ),
    linkStyle: body.copyWith(
      color: tokens.accent,
      decoration: TextDecoration.underline,
    ),
    quoteBorderColor: tokens.border,
    ruleColor: tokens.border,
  );
}

/// The markdown style of the current theme.
extension MarkdownStyleContext on BuildContext {
  MarkdownStyle get markdownStyle => markdownStyleFor(tokens);
}
