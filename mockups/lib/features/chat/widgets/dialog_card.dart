import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/data/mock_api.dart';
import '../../../core/data/providers.dart';
import '../../../core/models/chat_entry.dart';
import '../../../core/theme/theme_tokens.dart';

/// The blocking extension dialog, rendered above the composer.
///
/// It is a card and not a modal on purpose: a dialog must be answerable without
/// the user losing the conversation they are reading. The countdown is the
/// server's deadline, so it keeps running while the card is on screen and the
/// card disappears when nobody answered in time.
class DialogCard extends ConsumerStatefulWidget {
  const DialogCard({super.key, required this.sessionId, required this.request});

  final String sessionId;
  final DialogRequest request;

  @override
  ConsumerState<DialogCard> createState() => _DialogCardState();
}

class _DialogCardState extends ConsumerState<DialogCard> {
  Timer? _ticker;
  final _input = TextEditingController();

  @override
  void initState() {
    super.initState();
    // One tick a second is all the countdown needs; the deadline itself lives in
    // the request, so a missed tick cannot extend it.
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() {});
      }
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    _input.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final request = widget.request;
    final remaining = request.remainingSeconds(DateTime.now());
    final api = ref.read(mockApiProvider);
    return Container(
      margin: EdgeInsets.fromLTRB(
        tokens.spaceLg,
        0,
        tokens.spaceLg,
        tokens.spaceSm,
      ),
      padding: EdgeInsets.all(tokens.spaceMd),
      decoration: BoxDecoration(
        color: tokens.surface,
        borderRadius: BorderRadius.circular(tokens.radiusMd),
        border: Border.all(color: tokens.accent, width: 1.5),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.08),
            blurRadius: 12,
            offset: const Offset(0, 4),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.help_outline, size: 18, color: tokens.accent),
              SizedBox(width: tokens.spaceSm),
              Expanded(
                child: Text(request.title, style: theme.textTheme.titleSmall),
              ),
              Text(
                '${remaining}s',
                style: theme.textTheme.labelSmall?.copyWith(
                  color: remaining <= 5 ? tokens.error : tokens.textMuted,
                ),
              ),
              IconButton(
                tooltip: 'Cancel the dialog',
                onPressed: () =>
                    api.answerDialog(widget.sessionId, cancelled: true),
                icon: const Icon(Icons.close, size: 18),
              ),
            ],
          ),
          if (request.message != null) ...[
            SizedBox(height: tokens.spaceXs),
            SelectableText(request.message!, style: theme.textTheme.bodySmall),
          ],
          SizedBox(height: tokens.spaceMd),
          _answer(context, request, api),
        ],
      ),
    );
  }

  Widget _answer(BuildContext context, DialogRequest request, MockPiApi api) {
    final tokens = context.tokens;
    switch (request.method) {
      case DialogMethod.select:
        return Wrap(
          spacing: tokens.spaceSm,
          runSpacing: tokens.spaceSm,
          children: [
            for (final option in request.options)
              OutlinedButton(
                onPressed: () =>
                    api.answerDialog(widget.sessionId, value: option),
                child: Text(option),
              ),
          ],
        );
      case DialogMethod.confirm:
        return Row(
          children: [
            OutlinedButton(
              onPressed: () =>
                  api.answerDialog(widget.sessionId, confirmed: false),
              child: const Text('Deny'),
            ),
            SizedBox(width: tokens.spaceSm),
            FilledButton(
              onPressed: () =>
                  api.answerDialog(widget.sessionId, confirmed: true),
              child: const Text('Approve'),
            ),
          ],
        );
      case DialogMethod.input:
        return Row(
          children: [
            Expanded(
              child: TextField(
                controller: _input,
                autofocus: true,
                decoration: InputDecoration(
                  hintText: request.placeholder ?? 'Type an answer',
                ),
                onSubmitted: (value) =>
                    api.answerDialog(widget.sessionId, value: value),
              ),
            ),
            SizedBox(width: tokens.spaceSm),
            FilledButton(
              onPressed: () =>
                  api.answerDialog(widget.sessionId, value: _input.text),
              child: const Text('Send'),
            ),
          ],
        );
      case DialogMethod.editor:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              controller: _input,
              maxLines: 6,
              decoration: const InputDecoration(),
            ),
            SizedBox(height: tokens.spaceSm),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton(
                onPressed: () =>
                    api.answerDialog(widget.sessionId, value: _input.text),
                child: const Text('Save'),
              ),
            ),
          ],
        );
    }
  }
}
