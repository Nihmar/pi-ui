# piui_markdown

The markdown engine of **pi-ui**: one stack for chat rendering, tool output and
diffs, prompt templates, skills, `AGENTS.md` and host `.md` files. The Flutter
client and the mockup import this package and never a second markdown package.

## Upstream

The rendering and editing engine is ported from
[Niman](https://github.com/Nihmar/Niman) (MIT, © Nihmar), upstream-first: a
change to the engine is made in Niman and re-vendored here. Ported files carry a
header naming the upstream project, its licence and the upstream path, and the
pi-ui `README.md` lists the port. The M0 code in `src/markdown_parser.dart`,
`src/markdown_style.dart` and `src/markdown_view.dart` is written for pi-ui and
is the placeholder the Niman port replaces.

## Milestones

| # | Scope | State |
|---|---|---|
| M0 | The seam: `MarkdownView`, `MarkdownStyle`, the block/inline model and a read-only renderer for the chat subset (paragraphs, headings, fenced and inline code, strong, emphasis, links, quotes, lists, rules) | done |
| M1 | The ported read-only engine: Niman's block scanner and render layer behind the same `MarkdownView` API, with its conformance tests | planned |
| M2 | The editor (source / WYSIWYG) and the tool-diff views | planned |

## Usage

```dart
MarkdownView(
  message.text,
  style: appMarkdownStyle, // built by the app from its theme tokens
  onLinkTap: (url) => open(url),
);
```

`MarkdownStyle` is the visual contract: the engine never reads a hard-coded
colour from the application. `MarkdownStyle.fallback()` exists for tests and for
a caller without a theme.

## Checks

From this directory:

```bash
dart fix --apply && dart format . && flutter analyze && flutter test
```
