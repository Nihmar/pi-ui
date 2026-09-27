import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/git.dart';
import '../../core/api/providers.dart';
import '../../core/l10n/l10n.dart';
import '../../core/format.dart';
import '../../core/theme/breakpoints.dart';
import '../../core/theme/theme_tokens.dart';
import '../chat/widgets/diff_view.dart';

/// The git panel of one session: what changed, what is staged, and a commit.
///
/// It lives in a sheet (a dialog from the desktop breakpoint) because it belongs to a
/// session and is something a user opens, looks at and closes. The mutations are
/// operator-scoped **and** gated by the `git.write` setting, so a server with the setting
/// off answers `feature_disabled` — which the panel shows as the reason, not as a failure.
Future<void> showGitPanel(BuildContext context, String directory) {
  if (context.isExpanded) {
    return showDialog<void>(
      context: context,
      builder: (context) => Dialog(
        child: SizedBox(
          width: 720,
          height: 560,
          child: GitPanel(directory: directory),
        ),
      ),
    );
  }
  return showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) => SizedBox(
      height: MediaQuery.sizeOf(context).height * 0.85,
      child: GitPanel(directory: directory),
    ),
  );
}

/// The panel itself.
class GitPanel extends ConsumerStatefulWidget {
  const GitPanel({super.key, required this.directory});

  /// The repository: the session's working directory.
  final String directory;

  @override
  ConsumerState<GitPanel> createState() => _GitPanelState();
}

class _GitPanelState extends ConsumerState<GitPanel> {
  final _message = TextEditingController();
  String? _selected;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _message.dispose();
    super.dispose();
  }

  /// Runs one git mutation and refreshes what the panel shows.
  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      ref
        ..invalidate(gitStatusProvider(widget.directory))
        ..invalidate(gitLogProvider(widget.directory));
      if (mounted) {
        setState(() => _selected = null);
      }
    } on PiuiException catch (error) {
      setState(() {
        _error = error.code == ErrorCodes.featureDisabled
            ? context.l10n.gitWriteDisabled
            : error.message;
      });
    } finally {
      if (mounted) {
        setState(() => _busy = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final status = ref.watch(gitStatusProvider(widget.directory));
    return Padding(
      padding: EdgeInsets.fromLTRB(
        tokens.spaceLg,
        tokens.spaceSm,
        tokens.spaceLg,
        tokens.spaceLg,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.commit, size: 18, color: tokens.accent),
              SizedBox(width: tokens.spaceSm),
              Expanded(
                child: Text(
                  context.l10n.gitTitle,
                  style: theme.textTheme.titleMedium,
                ),
              ),
              if (status.value case final value?)
                Text(
                  [
                    value.branch,
                    if (value.detached) 'detached',
                    if (value.ahead > 0) '↑${value.ahead}',
                    if (value.behind > 0) '↓${value.behind}',
                  ].join(' · '),
                  style: theme.textTheme.labelSmall,
                ),
            ],
          ),
          SizedBox(height: tokens.spaceXs),
          Text(widget.directory, style: theme.textTheme.labelSmall),
          if (_error != null) ...[
            SizedBox(height: tokens.spaceSm),
            Text(
              _error!,
              style: theme.textTheme.bodySmall?.copyWith(color: tokens.error),
            ),
          ],
          SizedBox(height: tokens.spaceMd),
          Expanded(
            child: status.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (error, _) => Text(
                context.l10n.gitReadFailed('$error'),
                style: theme.textTheme.bodySmall,
              ),
              data: (value) => value.clean
                  ? Text(
                      context.l10n.gitNothingToCommit,
                      style: theme.textTheme.bodySmall,
                    )
                  : Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          flex: 3,
                          child: _Changes(
                            status: value,
                            selected: _selected,
                            busy: _busy,
                            onSelect: (path) =>
                                setState(() => _selected = path),
                            onStage: (change) => _run(
                              () => ref.read(clientProvider)!.gitStage(
                                widget.directory,
                                [change.path],
                              ),
                            ),
                          ),
                        ),
                        SizedBox(width: tokens.spaceMd),
                        Expanded(
                          flex: 4,
                          child: _selected == null
                              ? Text(
                                  context.l10n.gitSelectChange,
                                  style: theme.textTheme.bodySmall,
                                )
                              : _Diff(
                                  request: (
                                    dir: widget.directory,
                                    path: _selected!,
                                    staged: value.stagedChanges.any(
                                      (change) => change.path == _selected,
                                    ),
                                  ),
                                ),
                        ),
                      ],
                    ),
            ),
          ),
          SizedBox(height: tokens.spaceMd),
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _message,
                  decoration: InputDecoration(
                    labelText: context.l10n.gitCommitMessage,
                    hintText: context.l10n.gitCommitHint,
                  ),
                  onSubmitted: (_) => _commit(),
                ),
              ),
              SizedBox(width: tokens.spaceSm),
              FilledButton.icon(
                onPressed: _busy ? null : _commit,
                icon: const Icon(Icons.check, size: 16),
                label: Text(context.l10n.gitCommit),
              ),
            ],
          ),
        ],
      ),
    );
  }

  /// Commits the index: the message is the only thing git needs from the panel.
  Future<void> _commit() async {
    final message = _message.text.trim();
    if (message.isEmpty) {
      setState(() => _error = context.l10n.gitCommitNeedsMessage);
      return;
    }
    final client = ref.read(clientProvider);
    if (client == null) {
      return;
    }
    await _run(() async {
      await client.gitCommit(widget.directory, message);
      _message.clear();
    });
  }
}

/// The list of changed paths, staged and unstaged.
class _Changes extends StatelessWidget {
  const _Changes({
    required this.status,
    required this.selected,
    required this.busy,
    required this.onSelect,
    required this.onStage,
  });

  final GitStatus status;
  final String? selected;
  final bool busy;
  final ValueChanged<String> onSelect;
  final ValueChanged<GitChange> onStage;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return ListView.builder(
      itemCount: status.changes.length,
      itemBuilder: (context, index) {
        final change = status.changes[index];
        return ListTile(
          dense: true,
          selected: change.path == selected,
          leading: Icon(
            change.staged ? Icons.check_circle : Icons.circle_outlined,
            size: 16,
            color: change.staged ? tokens.success : tokens.textDim,
          ),
          title: Text(change.name, style: theme.textTheme.bodyMedium),
          subtitle: Text(
            change.path,
            style: theme.textTheme.labelSmall,
            overflow: TextOverflow.ellipsis,
          ),
          // The label and the stage button are one compact widget: a ListTile refuses a
          // trailing widget that wants the whole width, and on a phone it would.
          trailing: SizedBox(
            width: 96,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                Flexible(
                  child: Text(
                    change.label,
                    style: theme.textTheme.labelSmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (!change.staged)
                  IconButton(
                    tooltip: context.l10n.gitStage,
                    visualDensity: VisualDensity.compact,
                    onPressed: busy ? null : () => onStage(change),
                    icon: const Icon(Icons.add, size: 16),
                  ),
              ],
            ),
          ),
          onTap: () => onSelect(change.path),
        );
      },
    );
  }
}

/// One diff, rendered by the chat's own diff view: one renderer for tool results and for
/// the repository.
class _Diff extends ConsumerWidget {
  const _Diff({required this.request});

  final GitDiffRequest request;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final diff = ref.watch(gitDiffProvider(request));
    return diff.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => Text(
        context.l10n.gitDiffFailed('$error'),
        style: Theme.of(context).textTheme.bodySmall,
      ),
      data: (preview) => preview.lines.isEmpty
          ? Text(
              context.l10n.gitNoTextualChange,
              style: Theme.of(context).textTheme.bodySmall,
            )
          : SingleChildScrollView(child: DiffView(diff: preview)),
    );
  }
}

/// The relative time of a commit, for the log rows this panel grows when it gains a
/// history tab.
String commitTime(GitCommit commit) =>
    commit.at == null ? '' : relativeTime(commit.at!);
