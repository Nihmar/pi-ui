import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/providers.dart';
import '../../core/api/search.dart';
import '../../core/format.dart';
import '../../core/l10n/l10n.dart';
import '../../core/router.dart';
import '../../core/theme/theme_tokens.dart';
import '../../widgets/empty_state.dart';

/// The global search: ripgrep over the workspaces and a scan of pi's sessions.
///
/// One field, two scopes, one list. A file hit opens the file in the browser, a message
/// hit opens the session it belongs to — the same list, because a user searches for a
/// word, not for a subsystem.
class SearchScreen extends ConsumerStatefulWidget {
  const SearchScreen({super.key});

  @override
  ConsumerState<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends ConsumerState<SearchScreen> {
  final _controller = TextEditingController();
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    _controller.text = ref.read(searchRequestProvider).query;
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  /// Waits for a pause in the typing before asking the server: a search per keystroke
  /// would spend the request budget on prefixes nobody wanted to search for.
  void _onChanged(String value) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 300), () {
      if (mounted) {
        ref.read(searchRequestProvider.notifier).setQuery(value);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final request = ref.watch(searchRequestProvider);
    final results = ref.watch(searchResultsProvider);
    return Scaffold(
      appBar: AppBar(
        title: Text(context.l10n.searchTitle),
        actions: [
          IconButton(
            tooltip: context.l10n.searchNow,
            onPressed: () => ref
                .read(searchRequestProvider.notifier)
                .setQuery(_controller.text),
            icon: const Icon(Icons.search),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: EdgeInsets.fromLTRB(
              tokens.spaceLg,
              tokens.spaceSm,
              tokens.spaceLg,
              tokens.spaceSm,
            ),
            child: TextField(
              controller: _controller,
              autofocus: true,
              textInputAction: TextInputAction.search,
              onChanged: _onChanged,
              onSubmitted: (value) =>
                  ref.read(searchRequestProvider.notifier).setQuery(value),
              decoration: InputDecoration(
                labelText: context.l10n.search,
                hintText: context.l10n.searchHint,
                prefixIcon: const Icon(Icons.search),
              ),
            ),
          ),
          Wrap(
            spacing: tokens.spaceXs,
            children: [
              for (final scope in const ['files', 'messages'])
                FilterChip(
                  label: Text(
                    scope == 'files'
                        ? context.l10n.searchScopeFiles
                        : context.l10n.searchScopeMessages,
                  ),
                  selected: request.scope.contains(scope),
                  onSelected: (_) => ref
                      .read(searchRequestProvider.notifier)
                      .toggleScope(scope),
                ),
            ],
          ),
          SizedBox(height: tokens.spaceSm),
          Expanded(
            child: request.query.trim().length < 2
                ? EmptyState(
                    icon: Icons.search,
                    title: context.l10n.searchIntroTitle,
                    message: context.l10n.searchIntroMessage,
                  )
                : results.when(
                    loading: () =>
                        const Center(child: CircularProgressIndicator()),
                    error: (error, _) => EmptyState(
                      icon: Icons.error_outline,
                      title: context.l10n.searchFailedTitle,
                      message: '$error',
                    ),
                    data: (hits) => hits.isEmpty
                        ? EmptyState(
                            icon: Icons.search_off,
                            title: context.l10n.searchNoMatchTitle,
                            message: context.l10n.searchNoMatchMessage,
                          )
                        : _Results(hits: hits),
                  ),
          ),
        ],
      ),
    );
  }
}

/// The hit list: files and messages, in the order the server ranked them.
class _Results extends StatelessWidget {
  const _Results({required this.hits});

  final List<SearchHit> hits;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return ListView.separated(
      padding: EdgeInsets.symmetric(vertical: tokens.spaceSm),
      itemCount: hits.length,
      separatorBuilder: (context, _) =>
          Divider(height: 1, color: tokens.border),
      itemBuilder: (context, index) {
        final hit = hits[index];
        return ListTile(
          dense: true,
          leading: Icon(
            hit.isMessage
                ? Icons.chat_bubble_outline
                : Icons.description_outlined,
            size: 16,
            color: hit.isMessage ? tokens.accent : tokens.textDim,
          ),
          title: Text(hit.title, style: theme.textTheme.bodyMedium),
          subtitle: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                hit.text,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall,
              ),
              Text(hit.location, style: theme.textTheme.labelSmall),
            ],
          ),
          trailing: hit.isMessage && hit.at != null
              ? Text(relativeTime(hit.at!), style: theme.textTheme.labelSmall)
              : null,
          // A file opens in the browser, a message opens the session it came from: the
          // same list, and the difference is the tap.
          onTap: () => hit.isMessage
              ? context.go(Routes.chat(hit.sessionId ?? ''))
              : context.go(Routes.filePath(hit.path)),
        );
      },
    );
  }
}
