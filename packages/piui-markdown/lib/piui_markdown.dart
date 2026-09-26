/// The markdown engine of pi-ui (ADR 0009).
///
/// The app and the mockup import **this** library for every markdown surface —
/// chat messages, tool output and diffs, prompt templates, skills, `AGENTS.md`
/// and host `.md` files — and never a second markdown package.
///
/// The engine is ported from [Niman](https://github.com/Nihmar/Niman) (MIT),
/// upstream-first: a change to the engine is made in Niman and re-vendored here.
///
/// # Milestones
///
/// * **M0 (this one)** — the seam: [MarkdownView], [MarkdownStyle] and the
///   block/inline model, with a read-only renderer for the subset the chat
///   surface needs (paragraphs, headings, fenced and inline code, strong,
///   emphasis, links, quotes, lists, rules). The API is what the app codes
///   against; the renderer behind it is the placeholder the Niman port replaces.
/// * **M1** — the ported read-only engine: the Niman block scanner and render
///   layer behind the same [MarkdownView] API, with the conformance tests.
/// * **M2** — the editor (source / WYSIWYG) and the tool-diff views.
library;

export 'src/markdown_parser.dart'
    show
        MdBlock,
        MdCode,
        MdCodeBlock,
        MdEmphasis,
        MdHeading,
        MdInline,
        MdLink,
        MdListBlock,
        MdParagraph,
        MdQuoteBlock,
        MdRule,
        MdStrong,
        MdText,
        MarkdownParser;
export 'src/markdown_style.dart' show MarkdownStyle;
export 'src/markdown_view.dart' show MarkdownView;
