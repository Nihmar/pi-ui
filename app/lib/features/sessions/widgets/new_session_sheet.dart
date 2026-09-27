import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/providers.dart';
import '../../../core/l10n/l10n.dart';
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
///
/// The cwd is a host path: the client never browses the filesystem to pick it,
/// because the directory is the server's to validate (`POST /sessions` answers
/// `bad_request` with the reason).
class NewSessionSheet extends ConsumerStatefulWidget {
  const NewSessionSheet({super.key});

  @override
  ConsumerState<NewSessionSheet> createState() => _NewSessionSheetState();
}

class _NewSessionSheetState extends ConsumerState<NewSessionSheet> {
  final _cwd = TextEditingController();
  final _name = TextEditingController();
  String? _error;
  var _busy = false;

  @override
  void dispose() {
    _cwd.dispose();
    _name.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    final cwd = _cwd.text.trim();
    if (cwd.isEmpty) {
      setState(() => _error = context.l10n.workingDirectoryRequired);
      return;
    }
    final client = ref.read(clientProvider);
    if (client == null) {
      setState(() => _error = context.l10n.chatNotConnected);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final session = await client.createSession(
        cwd: cwd,
        name: _name.text.trim().isEmpty ? null : _name.text.trim(),
      );
      if (mounted) {
        Navigator.of(context).pop(session.id);
      }
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) {
        setState(() => _busy = false);
      }
    }
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
          Text(
            context.l10n.newSession,
            style: Theme.of(context).textTheme.titleMedium,
          ),
          SizedBox(height: tokens.spaceXs),
          Text(
            context.l10n.newSessionHint,
            style: Theme.of(context).textTheme.bodySmall,
          ),
          SizedBox(height: tokens.spaceLg),
          TextField(
            controller: _cwd,
            autofocus: true,
            autocorrect: false,
            decoration: InputDecoration(
              labelText: context.l10n.workingDirectory,
              hintText: context.l10n.workingDirectoryHint,
              errorText: _error,
              prefixIcon: const Icon(Icons.folder_outlined),
            ),
            onSubmitted: (_) => _create(),
          ),
          SizedBox(height: tokens.spaceMd),
          TextField(
            controller: _name,
            decoration: InputDecoration(
              labelText: context.l10n.nameOptional,
              hintText: context.l10n.nameHint,
              prefixIcon: const Icon(Icons.label_outline),
            ),
            onSubmitted: (_) => _create(),
          ),
          SizedBox(height: tokens.spaceLg),
          Row(
            children: [
              const Spacer(),
              TextButton(
                onPressed: () => Navigator.of(context).pop(),
                child: Text(context.l10n.cancel),
              ),
              SizedBox(width: tokens.spaceSm),
              FilledButton(
                onPressed: _busy ? null : _create,
                child: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text(context.l10n.create),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
