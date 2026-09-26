import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import 'markdown_parser.dart';
import 'markdown_style.dart';

/// Renders markdown as a column of selectable blocks.
///
/// This is the one widget every markdown surface of pi-ui uses — chat messages,
/// tool output, templates, skills, host files and `AGENTS.md`. The M0 renderer
/// covers the subset listed in the library documentation; a construct it does
/// not understand degrades to text instead of failing, which is what a chat
/// surface must do with a message a model got wrong.
class MarkdownView extends StatefulWidget {
  const MarkdownView(
    this.data, {
    super.key,
    this.style,
    this.selectable = true,
    this.onLinkTap,
  });

  /// The markdown source.
  final String data;

  /// The visual contract; [MarkdownStyle.fallback] when omitted.
  final MarkdownStyle? style;

  /// Whether the rendered text can be selected. Chat enables it; a small label
  /// (a status line, a diff header) can turn it off to keep the hit area calm.
  final bool selectable;

  /// Called with the destination of a tapped link. When null, links keep their
  /// style but are not tappable.
  final ValueChanged<String>? onLinkTap;

  @override
  State<MarkdownView> createState() => _MarkdownViewState();
}

class _MarkdownViewState extends State<MarkdownView> {
  // One recognizer per link span of the current build. They are recreated on
  // every build (a streaming message rebuilds constantly) and disposed here,
  // which is what keeps a long chat from leaking recognizers.
  final List<TapGestureRecognizer> _recognizers = [];

  @override
  void dispose() {
    _disposeRecognizers();
    super.dispose();
  }

  void _disposeRecognizers() {
    for (final recognizer in _recognizers) {
      recognizer.dispose();
    }
    _recognizers.clear();
  }

  @override
  Widget build(BuildContext context) {
    final style = widget.style ?? MarkdownStyle.fallback();
    _disposeRecognizers();
    final blocks = MarkdownParser.parse(widget.data);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: _blockWidgets(blocks, style),
    );
  }

  List<Widget> _blockWidgets(List<MdBlock> blocks, MarkdownStyle style) {
    final widgets = <Widget>[];
    for (var index = 0; index < blocks.length; index++) {
      final isLast = index == blocks.length - 1;
      widgets.add(
        Padding(
          padding: EdgeInsets.only(bottom: isLast ? 0 : style.blockSpacing),
          child: _blockWidget(blocks[index], style),
        ),
      );
    }
    return widgets;
  }

  Widget _blockWidget(MdBlock block, MarkdownStyle style) {
    return switch (block) {
      MdParagraph() => _prose(
        TextSpan(children: _inlineSpans(block.spans, style)),
        style,
      ),
      MdHeading() => _prose(
        TextSpan(children: _inlineSpans(block.spans, style)),
        style,
        base: style.headingStyles[(block.level - 1).clamp(0, 5)],
      ),
      MdCodeBlock() => _codeBlock(block, style),
      MdQuoteBlock() => Container(
        padding: const EdgeInsets.only(left: 12),
        decoration: BoxDecoration(
          border: Border(
            left: BorderSide(color: style.quoteBorderColor, width: 3),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: _blockWidgets(block.children, style),
        ),
      ),
      MdListBlock() => _list(block, style),
      MdRule() => Container(height: 1, color: style.ruleColor),
    };
  }

  Widget _prose(TextSpan span, MarkdownStyle style, {TextStyle? base}) {
    final effective = base ?? style.bodyStyle;
    if (!widget.selectable) {
      return Text.rich(span, style: effective);
    }
    return SelectableText.rich(span, style: effective);
  }

  Widget _codeBlock(MdCodeBlock block, MarkdownStyle style) {
    final children = <Widget>[];
    if (block.language != null) {
      children.add(
        Padding(
          padding: const EdgeInsets.only(bottom: 6),
          child: Text(block.language!, style: style.codeLabelStyle),
        ),
      );
    }
    final code = block.code.isEmpty
        ? Text('', style: style.codeStyle)
        : (widget.selectable
              ? SelectableText(block.code, style: style.codeStyle)
              : Text(block.code, style: style.codeStyle));
    children.add(code);
    return Container(
      padding: style.codePadding,
      decoration: BoxDecoration(
        color: style.codeBackground,
        border: Border.all(color: style.codeBorderColor),
        borderRadius: BorderRadius.circular(style.radius),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: children,
      ),
    );
  }

  Widget _list(MdListBlock block, MarkdownStyle style) {
    final rows = <Widget>[];
    for (var index = 0; index < block.items.length; index++) {
      final marker = block.ordered ? '${index + 1}.' : '•';
      rows.add(
        Padding(
          padding: EdgeInsets.only(
            bottom: index == block.items.length - 1 ? 0 : 4,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: style.listIndent,
                child: Text(marker, style: style.bodyStyle),
              ),
              Expanded(
                child: _prose(
                  TextSpan(children: _inlineSpans(block.items[index], style)),
                  style,
                ),
              ),
            ],
          ),
        ),
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: rows,
    );
  }

  List<InlineSpan> _inlineSpans(List<MdInline> spans, MarkdownStyle style) {
    final out = <InlineSpan>[];
    for (final span in spans) {
      switch (span) {
        case MdText():
          out.add(TextSpan(text: span.text));
        case MdCode():
          out.add(
            TextSpan(
              text: span.code,
              style: style.inlineCodeStyle.copyWith(
                backgroundColor: style.inlineCodeBackground,
              ),
            ),
          );
        case MdStrong():
          out.add(
            TextSpan(
              children: _inlineSpans(span.children, style),
              style: const TextStyle(fontWeight: FontWeight.bold),
            ),
          );
        case MdEmphasis():
          out.add(
            TextSpan(
              children: _inlineSpans(span.children, style),
              style: const TextStyle(fontStyle: FontStyle.italic),
            ),
          );
        case MdLink():
          out.add(
            TextSpan(
              children: _inlineSpans(span.children, style),
              style: style.linkStyle,
              recognizer: _linkRecognizer(span.destination),
            ),
          );
      }
    }
    return out;
  }

  GestureRecognizer? _linkRecognizer(String destination) {
    final onLinkTap = widget.onLinkTap;
    if (onLinkTap == null) {
      return null;
    }
    final recognizer = TapGestureRecognizer()
      ..onTap = () => onLinkTap(destination);
    _recognizers.add(recognizer);
    return recognizer;
  }
}
