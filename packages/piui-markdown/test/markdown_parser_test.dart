import 'package:flutter_test/flutter_test.dart';
import 'package:piui_markdown/piui_markdown.dart';

void main() {
  group('block parsing', () {
    test('blank lines separate paragraphs and soft breaks become spaces', () {
      final blocks = MarkdownParser.parse('one\ntwo\n\nthree');
      expect(blocks, hasLength(2));
      final first = blocks[0] as MdParagraph;
      expect((first.spans.single as MdText).text, 'one two');
      expect(((blocks[1] as MdParagraph).spans.single as MdText).text, 'three');
    });

    test('headings carry their level and inline content', () {
      final block =
          MarkdownParser.parse('### A **bold** title').single as MdHeading;
      expect(block.level, 3);
      expect(block.spans, hasLength(3));
      expect((block.spans[0] as MdText).text, 'A ');
      expect((block.spans[1] as MdStrong).children.single, isA<MdText>());
      expect((block.spans[2] as MdText).text, ' title');
    });

    test('a fenced block keeps its language and drops the fences', () {
      final block =
          MarkdownParser.parse('```dart\nvoid main() {}\n```').single
              as MdCodeBlock;
      expect(block.language, 'dart');
      expect(block.code, 'void main() {}');
    });

    test('a bare fence has no language', () {
      final block =
          MarkdownParser.parse('```\nplain\n```').single as MdCodeBlock;
      expect(block.language, isNull);
      expect(block.code, 'plain');
    });

    test('an unterminated fence still yields the code it saw', () {
      final block =
          MarkdownParser.parse('```bash\nls -la').single as MdCodeBlock;
      expect(block.code, 'ls -la');
    });

    test('rules are recognised in all three spellings', () {
      expect(MarkdownParser.parse('---'), [isA<MdRule>()]);
      expect(MarkdownParser.parse('***'), [isA<MdRule>()]);
      expect(MarkdownParser.parse('___'), [isA<MdRule>()]);
    });

    test('a quote is parsed recursively', () {
      final quote =
          MarkdownParser.parse('> # quoted\n> body').single as MdQuoteBlock;
      expect(quote.children, hasLength(2));
      expect((quote.children[0] as MdHeading).level, 1);
      expect(
        ((quote.children[1] as MdParagraph).spans.single as MdText).text,
        'body',
      );
    });

    test('bullet and ordered lists keep their kind and items', () {
      final bullets =
          MarkdownParser.parse('- one\n- two').single as MdListBlock;
      expect(bullets.ordered, isFalse);
      expect(bullets.items, hasLength(2));

      final ordered =
          MarkdownParser.parse('1. first\n2. second').single as MdListBlock;
      expect(ordered.ordered, isTrue);
      expect((ordered.items[1].single as MdText).text, 'second');
    });

    test('a continuation line folds into the item it follows', () {
      final list =
          MarkdownParser.parse('- one\n  and more').single as MdListBlock;
      expect(list.items, hasLength(1));
      final text = list.items.single
          .map((span) => (span as MdText).text)
          .join();
      expect(text, 'one and more');
    });

    test('an ordered list after a bullet list starts a new block', () {
      final blocks = MarkdownParser.parse('- one\n1. first');
      expect(blocks, hasLength(2));
      expect((blocks[0] as MdListBlock).ordered, isFalse);
      expect((blocks[1] as MdListBlock).ordered, isTrue);
    });

    test('CRLF and CR documents parse like LF ones', () {
      final blocks = MarkdownParser.parse('# title\r\n\r\nbody\r');
      expect(blocks, hasLength(2));
      expect((blocks[0] as MdHeading).level, 1);
    });
  });

  group('inline parsing', () {
    test('inline code wins over emphasis inside it', () {
      final spans = MarkdownParser.parseInline('a `*x*` b');
      expect((spans[0] as MdText).text, 'a ');
      expect((spans[1] as MdCode).code, '*x*');
      expect((spans[2] as MdText).text, ' b');
    });

    test('strong and emphasis are separate spans', () {
      final spans = MarkdownParser.parseInline('**bold** and *italic*');
      expect(spans, hasLength(3));
      expect(spans[0], isA<MdStrong>());
      expect((spans[1] as MdText).text, ' and ');
      expect(spans[2], isA<MdEmphasis>());
    });

    test('links carry their destination and parsed label', () {
      final link =
          MarkdownParser.parseInline('[the **docs**](https://x.example)').single
              as MdLink;
      expect(link.destination, 'https://x.example');
      expect((link.children[1] as MdStrong), isA<MdStrong>());
    });

    test('an underscore inside a word is not emphasis', () {
      final spans = MarkdownParser.parseInline('snake_case_name');
      expect(spans.single, isA<MdText>());
      expect((spans.single as MdText).text, 'snake_case_name');
    });

    test('a backslash escapes the next character', () {
      final spans = MarkdownParser.parseInline(r'a \*literal\* b');
      expect((spans.single as MdText).text, 'a *literal* b');
    });

    test('an unmatched marker stays literal', () {
      final spans = MarkdownParser.parseInline('2 * 3 = 6');
      expect((spans.single as MdText).text, '2 * 3 = 6');
    });
  });
}
