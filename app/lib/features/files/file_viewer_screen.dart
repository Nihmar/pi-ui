import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:piui_markdown/piui_markdown.dart';

import '../../core/api/files.dart';
import '../../core/api/providers.dart';
import '../../core/format.dart';
import '../../core/theme/markdown_theme.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';

/// One file, rendered with the shared markdown engine when it is markdown and as
/// monospace text otherwise.
///
/// It is read-only in this slice: the write endpoint exists (`PUT /files/write`, with the
/// conditional hash), but an editor is its own feature — the same reason the markdown
/// editor is a separate milestone in ADR 0009.
class FileViewerScreen extends ConsumerWidget {
  const FileViewerScreen({super.key, required this.path});

  /// The absolute host path of the file.
  final String path;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final content = ref.watch(fileContentProvider(path));
    return content.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => EmptyState(
        icon: Icons.error_outline,
        title: 'That file cannot be read',
        message: '$error',
      ),
      data: (file) => _FileBody(file: file, path: path),
    );
  }
}

class _FileBody extends ConsumerWidget {
  const _FileBody({required this.file, required this.path});

  final FileContent file;
  final String path;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          padding: EdgeInsets.symmetric(
            horizontal: tokens.spaceLg,
            vertical: tokens.spaceSm,
          ),
          decoration: BoxDecoration(
            color: tokens.surface,
            border: Border(bottom: BorderSide(color: tokens.border)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    file.entry.isMarkdown
                        ? Icons.article_outlined
                        : Icons.description_outlined,
                    size: 16,
                    color: tokens.textDim,
                  ),
                  SizedBox(width: tokens.spaceXs),
                  Expanded(
                    child: Text(
                      file.entry.name,
                      style: theme.textTheme.titleSmall,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  IconButton(
                    tooltip: 'Reload from the host',
                    onPressed: () => ref.invalidate(fileContentProvider(path)),
                    icon: const Icon(Icons.refresh, size: 18),
                  ),
                ],
              ),
              SizedBox(height: tokens.spaceXs),
              Wrap(
                spacing: tokens.spaceSm,
                runSpacing: tokens.spaceXs,
                children: [
                  Text(
                    formatBytes(file.entry.size),
                    style: theme.textTheme.labelSmall,
                  ),
                  if (file.entry.mode.isNotEmpty)
                    Text(file.entry.mode, style: theme.textTheme.labelSmall),
                  if (file.entry.sha256 case final sha?)
                    Tooltip(
                      message: 'sha256 $sha',
                      child: Text(
                        'sha256 ${sha.substring(0, sha.length.clamp(0, 12))}',
                        style: theme.textTheme.labelSmall,
                      ),
                    ),
                  if (file.entry.modTime case final at?)
                    Text(relativeTime(at), style: theme.textTheme.labelSmall),
                ],
              ),
              SizedBox(height: tokens.spaceXs),
              SelectableText(
                file.entry.path,
                style: theme.textTheme.labelSmall,
              ),
            ],
          ),
        ),
        Expanded(child: content(context)),
      ],
    );
  }

  Widget content(BuildContext context) {
    final tokens = context.tokens;
    final text = file.text;
    if (text == null) {
      return EmptyState(
        icon: Icons.data_object,
        title: 'Binary file',
        message:
            '${file.entry.name} is not valid UTF-8, so there is nothing to render. '
            'Its content travels as base64 when a viewer wants the bytes.',
      );
    }
    if (file.entry.isMarkdown) {
      return SingleChildScrollView(
        padding: EdgeInsets.all(tokens.spaceLg),
        child: MarkdownView(text, style: context.markdownStyle),
      );
    }
    return SingleChildScrollView(
      padding: EdgeInsets.all(tokens.spaceLg),
      child: SelectableText(
        text,
        style: TextStyle(
          fontFamily: 'monospace',
          fontSize: 13,
          height: 1.45,
          color: Theme.of(context).colorScheme.onSurface,
        ),
      ),
    );
  }
}
