/// Block and inline model of a markdown document, and the M0 parser.
///
/// The model is deliberately small and closed: [MarkdownView] renders it, and the
/// M1 Niman port fills the same shapes with its own scanner. Nothing here touches
/// Flutter, so the parser is unit-testable without a binding.
library;

/// One top-level block of a document.
sealed class MdBlock {
  const MdBlock();
}

/// A run of inline content: the most common block.
final class MdParagraph extends MdBlock {
  const MdParagraph(this.spans);

  /// The inline content, in source order.
  final List<MdInline> spans;
}

/// An ATX heading (`#` … `######`).
final class MdHeading extends MdBlock {
  const MdHeading(this.level, this.spans) : assert(level >= 1 && level <= 6);

  /// Heading level, 1 (largest) to 6.
  final int level;

  /// The inline content, in source order.
  final List<MdInline> spans;
}

/// A fenced code block. The language is the fence's info string, verbatim.
final class MdCodeBlock extends MdBlock {
  const MdCodeBlock(this.code, {this.language});

  /// The code, without the fences and without a trailing newline.
  final String code;

  /// The fence info string (`dart`, `bash`, …), or null when the fence was bare.
  final String? language;
}

/// A block quote: the quoted lines, parsed recursively.
final class MdQuoteBlock extends MdBlock {
  const MdQuoteBlock(this.children);

  /// The quoted document.
  final List<MdBlock> children;
}

/// A bullet or ordered list. Every item is one line of inline content in M0; a
/// continuation line is folded into the item it follows.
final class MdListBlock extends MdBlock {
  const MdListBlock({required this.ordered, required this.items});

  /// True for `1.`, `2.` …; false for `-`, `*`, `+`.
  final bool ordered;

  /// One entry per item, in source order.
  final List<List<MdInline>> items;
}

/// A thematic break (`---`, `***`, `___`).
final class MdRule extends MdBlock {
  const MdRule();
}

/// One inline fragment of a block.
sealed class MdInline {
  const MdInline();
}

/// Literal text.
final class MdText extends MdInline {
  const MdText(this.text);

  /// The text, with escapes resolved.
  final String text;
}

/// An inline code span (`` `code` ``).
final class MdCode extends MdInline {
  const MdCode(this.code);

  /// The code, without the backticks.
  final String code;
}

/// Strong emphasis (`**text**` or `__text__`).
final class MdStrong extends MdInline {
  const MdStrong(this.children);

  /// The emphasised content.
  final List<MdInline> children;
}

/// Emphasis (`*text*` or `_text_`).
final class MdEmphasis extends MdInline {
  const MdEmphasis(this.children);

  /// The emphasised content.
  final List<MdInline> children;
}

/// A link (`[label](destination)`).
final class MdLink extends MdInline {
  const MdLink(this.destination, this.children);

  /// The URL or path the link points at.
  final String destination;

  /// The link label, parsed for nested emphasis and code.
  final List<MdInline> children;
}

/// Parses the markdown subset of milestone M0.
///
/// The grammar is CommonMark's for the constructs it covers; a document the
/// parser does not understand degrades to text, which is what a chat surface
/// must do with a message a model got wrong.
abstract final class MarkdownParser {
  /// Parses [source] into blocks, in order.
  static List<MdBlock> parse(String source) {
    final lines = source
        .replaceAll('\r\n', '\n')
        .replaceAll('\r', '\n')
        .split('\n');
    final blocks = <MdBlock>[];
    var index = 0;

    while (index < lines.length) {
      final line = lines[index];
      if (line.trim().isEmpty) {
        index++;
        continue;
      }

      final fence = _fenceStart.firstMatch(line);
      if (fence != null) {
        final language = fence.group(1)?.trim();
        final code = <String>[];
        index++;
        while (index < lines.length && !_fenceEnd.hasMatch(lines[index])) {
          code.add(lines[index]);
          index++;
        }
        if (index < lines.length) {
          index++; // the closing fence
        }
        blocks.add(
          MdCodeBlock(
            code.join('\n'),
            language: language == null || language.isEmpty ? null : language,
          ),
        );
        continue;
      }

      final heading = _heading.firstMatch(line);
      if (heading != null) {
        final level = heading.group(1)!.length;
        blocks.add(MdHeading(level, parseInline(heading.group(2)!.trim())));
        index++;
        continue;
      }

      if (_rule.hasMatch(line)) {
        blocks.add(const MdRule());
        index++;
        continue;
      }

      if (line.trimLeft().startsWith('>')) {
        final quoted = <String>[];
        while (index < lines.length &&
            (lines[index].trimLeft().startsWith('>') ||
                lines[index].trim().isEmpty)) {
          final content = lines[index].trimLeft();
          if (content.startsWith('>')) {
            quoted.add(content.substring(1).replaceFirst(RegExp('^ '), ''));
          } else {
            quoted.add('');
          }
          index++;
        }
        blocks.add(MdQuoteBlock(parse(quoted.join('\n'))));
        continue;
      }

      final item = _listItem.firstMatch(line);
      if (item != null) {
        final ordered = RegExp(r'\d').hasMatch(item.group(1)!);
        final items = <List<MdInline>>[];
        while (index < lines.length) {
          final next = _listItem.firstMatch(lines[index]);
          if (next != null) {
            if (RegExp(r'\d').hasMatch(next.group(1)!) != ordered) {
              break;
            }
            items.add(parseInline(next.group(2)!.trim()));
            index++;
            continue;
          }
          // A continuation line folds into the item it follows.
          if (lines[index].trim().isNotEmpty &&
              items.isNotEmpty &&
              !_startsBlock(lines[index])) {
            final continuation = parseInline(lines[index].trim());
            items[items.length - 1] = [
              ...items.last,
              const MdText(' '),
              ...continuation,
            ];
            index++;
            continue;
          }
          break;
        }
        blocks.add(MdListBlock(ordered: ordered, items: items));
        continue;
      }

      final paragraph = <String>[];
      while (index < lines.length &&
          lines[index].trim().isNotEmpty &&
          !_startsBlock(lines[index])) {
        paragraph.add(lines[index].trim());
        index++;
      }
      blocks.add(MdParagraph(parseInline(paragraph.join(' '))));
    }

    return blocks;
  }

  /// Parses one line of inline content.
  static List<MdInline> parseInline(String source) {
    final spans = <MdInline>[];
    final buffer = StringBuffer();
    var index = 0;

    void flush() {
      if (buffer.isNotEmpty) {
        spans.add(MdText(buffer.toString()));
        buffer.clear();
      }
    }

    while (index < source.length) {
      final char = source[index];

      if (char == r'\' && index + 1 < source.length) {
        buffer.write(source[index + 1]);
        index += 2;
        continue;
      }

      if (char == '`') {
        final end = source.indexOf('`', index + 1);
        if (end > index) {
          flush();
          spans.add(MdCode(source.substring(index + 1, end)));
          index = end + 1;
          continue;
        }
      }

      if (char == '[') {
        final close = source.indexOf(']', index + 1);
        if (close > index &&
            close + 1 < source.length &&
            source[close + 1] == '(') {
          final paren = source.indexOf(')', close + 2);
          if (paren > close) {
            flush();
            spans.add(
              MdLink(
                source.substring(close + 2, paren).trim(),
                parseInline(source.substring(index + 1, close)),
              ),
            );
            index = paren + 1;
            continue;
          }
        }
      }

      if (char == '*' || char == '_') {
        final marker = source.startsWith(char + char, index)
            ? char + char
            : char;
        // An underscore inside a word is a word character, not emphasis
        // (snake_case must survive a chat message).
        final opens =
            char != '_' ||
            index == 0 ||
            !_isWordChar(source.codeUnitAt(index - 1));
        if (opens) {
          final end = source.indexOf(marker, index + marker.length);
          final closes =
              end > index &&
              (char != '_' ||
                  end + 1 >= source.length ||
                  !_isWordChar(source.codeUnitAt(end + 1)));
          if (closes) {
            flush();
            final inner = parseInline(
              source.substring(index + marker.length, end),
            );
            spans.add(marker.length == 2 ? MdStrong(inner) : MdEmphasis(inner));
            index = end + marker.length;
            continue;
          }
        }
      }

      buffer.write(char);
      index++;
    }

    flush();
    return spans;
  }

  static bool _startsBlock(String line) {
    return _fenceStart.hasMatch(line) ||
        _heading.hasMatch(line) ||
        _rule.hasMatch(line) ||
        line.trimLeft().startsWith('>') ||
        _listItem.hasMatch(line);
  }

  static bool _isWordChar(int codeUnit) {
    return (codeUnit >= 0x30 && codeUnit <= 0x39) ||
        (codeUnit >= 0x41 && codeUnit <= 0x5A) ||
        (codeUnit >= 0x61 && codeUnit <= 0x7A) ||
        codeUnit == 0x5F;
  }

  static final RegExp _fenceStart = RegExp(r'^\s*```\s*(.*)$');
  static final RegExp _fenceEnd = RegExp(r'^\s*```\s*$');
  static final RegExp _heading = RegExp(r'^\s*(#{1,6})\s+(.*)$');
  static final RegExp _rule = RegExp(r'^\s*([-*_])(\s*\1){2,}\s*$');
  static final RegExp _listItem = RegExp(r'^\s*([-*+]|\d+[.)])\s+(.*)$');
}
