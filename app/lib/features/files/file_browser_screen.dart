import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/files.dart';
import '../../core/api/providers.dart';
import '../../core/l10n/l10n.dart';
import '../../core/router.dart';
import '../../core/theme/breakpoints.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';
import 'file_viewer_screen.dart';
import 'widgets/file_entry_tile.dart';

/// The files branch: the workspaces the server allows, browsed.
///
/// On a phone it is the listing, full screen, and a file opens its own route. From the
/// desktop breakpoint it is the master pane of a master/detail layout with the viewer
/// beside it, exactly like the sessions branch: the layout, not the route, decides how
/// much is on screen.
class FileBrowserScreen extends ConsumerStatefulWidget {
  const FileBrowserScreen({super.key, this.initialPath});

  /// The directory a deep link asked for; the first root when there is none.
  final String? initialPath;

  @override
  ConsumerState<FileBrowserScreen> createState() => _FileBrowserScreenState();
}

class _FileBrowserScreenState extends ConsumerState<FileBrowserScreen> {
  /// The directory being browsed, or null while the first root is still loading.
  String? _path;

  @override
  void initState() {
    super.initState();
    _path = widget.initialPath;
  }

  @override
  void didUpdateWidget(covariant FileBrowserScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.initialPath != oldWidget.initialPath) {
      _path = widget.initialPath;
    }
  }

  @override
  Widget build(BuildContext context) {
    final roots = ref.watch(workspacesProvider);
    return Scaffold(
      appBar: AppBar(
        title: Text(context.l10n.filesTitle),
        actions: [
          IconButton(
            tooltip: context.l10n.refresh,
            onPressed: () {
              final path = _path;
              if (path != null) {
                ref.invalidate(directoryProvider(path));
              }
              ref.invalidate(workspacesProvider);
            },
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: roots.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (error, _) => EmptyState(
          icon: Icons.folder_off_outlined,
          title: 'No workspace',
          message: 'This server has no workspace to browse.\n\n$error',
        ),
        data: (list) {
          if (list.isEmpty) {
            return EmptyState(
              icon: Icons.folder_off_outlined,
              title: context.l10n.noWorkspaceTitle,
              message: context.l10n.noWorkspaceFlagHint,
            );
          }
          // The first root is the default; a deep link may name another one.
          final current = _path ??= list.first.path;
          if (context.isExpanded) {
            return Row(
              children: [
                SizedBox(
                  width: 380,
                  child: _Listing(
                    roots: list,
                    path: current,
                    selectedFile: null,
                    onOpenDirectory: (path) =>
                        context.go(Routes.filesPath(path)),
                    onOpenFile: (entry) =>
                        context.go(Routes.filePath(entry.path)),
                  ),
                ),
                const VerticalDivider(width: 1),
                Expanded(
                  child: EmptyState(
                    icon: Icons.description_outlined,
                    title: context.l10n.selectAFile,
                    message: context.l10n.pickAFileHint,
                  ),
                ),
              ],
            );
          }
          return _Listing(
            roots: list,
            path: current,
            selectedFile: null,
            onOpenDirectory: (path) => context.go(Routes.filesPath(path)),
            onOpenFile: (entry) => context.go(Routes.filePath(entry.path)),
          );
        },
      ),
    );
  }
}

/// The `view` route: a file, either beside the listing on a desktop or on its own on a
/// phone.
class FileDetailScreen extends ConsumerStatefulWidget {
  const FileDetailScreen({super.key, required this.path});

  final String path;

  /// The directory a file belongs to: what a back navigation and the master pane show.
  static String directoryOf(String path) {
    final index = path.lastIndexOf('/');
    if (index <= 0) {
      return path;
    }
    return path.substring(0, index);
  }

  @override
  ConsumerState<FileDetailScreen> createState() => _FileDetailScreenState();
}

class _FileDetailScreenState extends ConsumerState<FileDetailScreen> {
  @override
  Widget build(BuildContext context) {
    if (context.isExpanded) {
      final roots =
          ref.watch(workspacesProvider).value ?? const <WorkspaceRoot>[];
      final directory = FileDetailScreen.directoryOf(widget.path);
      return Row(
        children: [
          SizedBox(
            width: 380,
            child: roots.isEmpty
                ? EmptyState(
                    icon: Icons.folder_off_outlined,
                    title: context.l10n.noWorkspaceTitle,
                    message: context.l10n.noWorkspaceMessage,
                  )
                : _Listing(
                    roots: roots,
                    path: directory,
                    selectedFile: widget.path,
                    onOpenDirectory: (path) =>
                        context.go(Routes.filesPath(path)),
                    onOpenFile: (entry) =>
                        context.go(Routes.filePath(entry.path)),
                  ),
          ),
          const VerticalDivider(width: 1),
          Expanded(child: FileViewerScreen(path: widget.path)),
        ],
      );
    }
    return Scaffold(
      appBar: AppBar(title: Text(_name(widget.path))),
      body: SafeArea(child: FileViewerScreen(path: widget.path)),
    );
  }

  static String _name(String path) {
    final parts = path.split('/').where((part) => part.isNotEmpty);
    return parts.isEmpty ? path : parts.last;
  }
}

/// The listing itself: the root picker, the breadcrumb and the entries.
class _Listing extends ConsumerWidget {
  const _Listing({
    required this.roots,
    required this.path,
    required this.selectedFile,
    required this.onOpenDirectory,
    required this.onOpenFile,
  });

  final List<WorkspaceRoot> roots;
  final String path;
  final String? selectedFile;
  final ValueChanged<String> onOpenDirectory;
  final ValueChanged<FsEntry> onOpenFile;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tokens = context.tokens;
    final directory = ref.watch(directoryProvider(path));
    final crumbs = _breadcrumbs(roots, path);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(
            tokens.spaceMd,
            tokens.spaceSm,
            tokens.spaceMd,
            0,
          ),
          child: Row(
            children: [
              Expanded(
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      for (var index = 0; index < crumbs.length; index++) ...[
                        if (index > 0)
                          Icon(
                            Icons.chevron_right,
                            size: 14,
                            color: tokens.textDim,
                          ),
                        TextButton(
                          style: TextButton.styleFrom(
                            padding: EdgeInsets.symmetric(
                              horizontal: tokens.spaceXs,
                            ),
                            minimumSize: Size.zero,
                            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                          ),
                          onPressed: () => onOpenDirectory(crumbs[index].path),
                          child: Text(crumbs[index].label),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
        Expanded(
          child: directory.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (error, _) => EmptyState(
              icon: Icons.error_outline,
              title: context.l10n.directoryFailedTitle,
              message: '$error',
            ),
            data: (entries) {
              if (entries.isEmpty) {
                return EmptyState(
                  icon: Icons.folder_open,
                  title: context.l10n.emptyDirectoryTitle,
                  message: context.l10n.emptyDirectoryMessage,
                );
              }
              return ListView.builder(
                padding: EdgeInsets.symmetric(vertical: tokens.spaceSm),
                itemCount: entries.length,
                itemBuilder: (context, index) {
                  final entry = entries[index];
                  return FileEntryTile(
                    entry: entry,
                    selected: entry.path == selectedFile,
                    onTap: () => entry.isDir
                        ? onOpenDirectory(entry.path)
                        : onOpenFile(entry),
                  );
                },
              );
            },
          ),
        ),
      ],
    );
  }

  /// The path from the root to the current directory, as a list of crumbs.
  static List<({String label, String path})> _breadcrumbs(
    List<WorkspaceRoot> roots,
    String path,
  ) {
    WorkspaceRoot? root;
    for (final candidate in roots) {
      if (path == candidate.path || path.startsWith('${candidate.path}/')) {
        root = candidate;
        break;
      }
    }
    if (root == null) {
      return [(label: '/', path: path)];
    }
    final crumbs = <({String label, String path})>[
      (label: root.label, path: root.path),
    ];
    var current = root.path;
    for (final segment
        in path
            .substring(root.path.length)
            .split('/')
            .where((part) => part.isNotEmpty)) {
      current = '$current/$segment';
      crumbs.add((label: segment, path: current));
    }
    return crumbs;
  }
}
