import 'package:flutter/material.dart';

import '../../../core/api/files.dart';
import '../../../core/format.dart';
import '../../../core/theme/theme_tokens.dart';

/// One row of a directory listing: a folder to open or a file to read.
///
/// It is presentational on purpose — the same widget is the phone list and the desktop
/// master pane, and what a tap does is the screen's business.
class FileEntryTile extends StatelessWidget {
  const FileEntryTile({
    super.key,
    required this.entry,
    required this.selected,
    required this.onTap,
  });

  final FsEntry entry;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: tokens.spaceSm, vertical: 2),
      child: Material(
        color: selected ? tokens.selectedBg : Colors.transparent,
        borderRadius: BorderRadius.circular(tokens.radiusSm),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(tokens.radiusSm),
          child: Padding(
            padding: EdgeInsets.symmetric(
              horizontal: tokens.spaceSm,
              vertical: tokens.spaceXs,
            ),
            child: Row(
              children: [
                Icon(
                  iconOf(entry),
                  size: 16,
                  color: entry.isDir ? tokens.accent : tokens.textDim,
                ),
                SizedBox(width: tokens.spaceSm),
                Expanded(
                  child: Text(
                    entry.name,
                    style: theme.textTheme.bodyMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (!entry.isDir) ...[
                  SizedBox(width: tokens.spaceSm),
                  Text(
                    formatBytes(entry.size),
                    style: theme.textTheme.labelSmall,
                  ),
                ],
                if (entry.modTime != null) ...[
                  SizedBox(width: tokens.spaceSm),
                  Text(
                    relativeTime(entry.modTime!),
                    style: theme.textTheme.labelSmall,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// The glyph a row shows: a folder, or a hint of what the file is.
IconData iconOf(FsEntry entry) {
  if (entry.isDir) {
    return Icons.folder_outlined;
  }
  return switch (entry.extension) {
    'md' || 'markdown' => Icons.article_outlined,
    'json' || 'yaml' || 'yml' || 'toml' || 'ini' => Icons.tune,
    'dart' ||
    'go' ||
    'ts' ||
    'tsx' ||
    'js' ||
    'py' ||
    'rs' ||
    'sh' => Icons.code,
    'png' ||
    'jpg' ||
    'jpeg' ||
    'gif' ||
    'webp' ||
    'svg' => Icons.image_outlined,
    _ => Icons.description_outlined,
  };
}
