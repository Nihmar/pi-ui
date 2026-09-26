import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/data/providers.dart';
import '../../../core/theme/breakpoints.dart';
import '../../../core/theme/theme_tokens.dart';

/// Asks for a working directory and a name and creates the session.
///
/// A bottom sheet on a phone, a dialog on a desktop: the same form either way.
/// Returns the new session id, or null when the user cancelled.
Future<String?> showNewSessionSheet(BuildContext context) {
  if (context.isExpanded) {
    return showDialog<String>(
      context: context,
      builder: (context) =>
          const Dialog(child: SizedBox(width: 480, child: NewSessionSheet())),
    );
  }
  return showModalBottomSheet<String>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) => const NewSessionSheet(),
  );
}

/// The session-creation form.
class NewSessionSheet extends ConsumerStatefulWidget {
  const NewSessionSheet({super.key});

  @override
  ConsumerState<NewSessionSheet> createState() => _NewSessionSheetState();
}

class _NewSessionSheetState extends ConsumerState<NewSessionSheet> {
  final _cwd = TextEditingController();
  final _name = TextEditingController();
  String? _error;

  /// The roots the mockup offers instead of a host browser. The real client
  /// gets them from the server's allowed roots and its session history.
  static const _recentRoots = [
    '/home/user/Projects/pi-ui',
    '/home/user/Projects/Niman',
    '/home/user/Projects',
  ];

  @override
  void dispose() {
    _cwd.dispose();
    _name.dispose();
    super.dispose();
  }

  void _create() {
    final cwd = _cwd.text.trim();
    if (cwd.isEmpty) {
      setState(() => _error = 'A working directory is required.');
      return;
    }
    if (!cwd.startsWith('/')) {
      setState(() => _error = 'The directory must be an absolute path.');
      return;
    }
    final session = ref
        .read(mockApiProvider)
        .createSession(
          cwd: cwd,
          name: _name.text.trim().isEmpty ? null : _name.text.trim(),
        );
    Navigator.of(context).pop(session.id);
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final bottom = MediaQuery.viewInsetsOf(context).bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(
        tokens.spaceLg,
        tokens.spaceSm,
        tokens.spaceLg,
        tokens.spaceLg + bottom,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('New session', style: Theme.of(context).textTheme.titleMedium),
          SizedBox(height: tokens.spaceXs),
          Text(
            'pi runs in a host directory; the client never reads provider secrets.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          SizedBox(height: tokens.spaceLg),
          TextField(
            controller: _cwd,
            autofocus: true,
            decoration: InputDecoration(
              labelText: 'Working directory',
              hintText: '/home/user/Projects/my-project',
              errorText: _error,
              prefixIcon: const Icon(Icons.folder_outlined),
              suffixIcon: IconButton(
                tooltip: 'Browse the host (lands with the Files screens)',
                onPressed: () => ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(
                    content: Text('Host browser lands with the Files screens'),
                  ),
                ),
                icon: const Icon(Icons.folder_open),
              ),
            ),
            onSubmitted: (_) => _create(),
          ),
          SizedBox(height: tokens.spaceSm),
          Wrap(
            spacing: tokens.spaceXs,
            runSpacing: tokens.spaceXs,
            children: [
              for (final root in _recentRoots)
                ActionChip(
                  label: Text(root),
                  onPressed: () => setState(() {
                    _cwd.text = root;
                    _error = null;
                  }),
                ),
            ],
          ),
          SizedBox(height: tokens.spaceLg),
          TextField(
            controller: _name,
            decoration: const InputDecoration(
              labelText: 'Name (optional)',
              hintText: 'pi-ui',
              prefixIcon: Icon(Icons.label_outline),
            ),
            onSubmitted: (_) => _create(),
          ),
          SizedBox(height: tokens.spaceLg),
          Row(
            children: [
              const Spacer(),
              TextButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('Cancel'),
              ),
              SizedBox(width: tokens.spaceSm),
              FilledButton(onPressed: _create, child: const Text('Create')),
            ],
          ),
        ],
      ),
    );
  }
}
