import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/providers.dart';
import '../../../core/l10n/l10n.dart';
import '../../../core/models/chat_entry.dart';
import '../../../core/theme/theme_tokens.dart';

/// The blocking extension dialog, rendered above the composer.
///
/// It is a card and not a modal on purpose: a dialog must be answerable without
/// the user losing the conversation they are reading. The countdown is the
/// server's deadline, so it keeps running while the card is on screen and the
/// card disappears when nobody answered in time.
class DialogCard extends ConsumerStatefulWidget {
  const DialogCard({
    super.key,
    required this.sessionId,
    required this.request,
    required this.onAnswered,
  });

  final String sessionId;
  final DialogRequest request;

  /// Called with the request id once the user answered or cancelled it.
  final ValueChanged<String> onAnswered;

  @override
  ConsumerState<DialogCard> createState() => _DialogCardState();
}

class _DialogCardState extends ConsumerState<DialogCard> {
  Timer? _ticker;
  final _input = TextEditingController();

  @override
  void initState() {
    super.initState();
    _input.text = widget.request.prefill ?? '';
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

  /// Answers the dialog and tells the screen, so the card goes at once instead
  /// of waiting for the answer to come back.
  ///
  /// An answer that could not go out keeps the card: the dialog is still open on the
  /// server, and pretending it was answered would lose the run.
  void _answer({String? value, bool? confirmed, bool cancelled = false}) {
    final sent =
        ref
            .read(socketProvider)
            ?.uiResponse(
              sessionId: widget.sessionId,
              requestId: widget.request.id,
              value: value,
              confirmed: confirmed,
              cancelled: cancelled ? true : null,
            ) ??
        false;
    if (!sent) {
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(context.l10n.connectionIdle)));
      return;
    }
    widget.onAnswered(widget.request.id);
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final request = widget.request;
    final remaining = request.remainingSeconds(DateTime.now());
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
                tooltip: context.l10n.dialogCancel,
                onPressed: () => _answer(cancelled: true),
                icon: const Icon(Icons.close, size: 18),
              ),
            ],
          ),
          if (request.message != null) ...[
            SizedBox(height: tokens.spaceXs),
            SelectableText(request.message!, style: theme.textTheme.bodySmall),
          ],
          SizedBox(height: tokens.spaceMd),
          _answerRow(context, request),
        ],
      ),
    );
  }

  Widget _answerRow(BuildContext context, DialogRequest request) {
    final tokens = context.tokens;
    switch (request.method) {
      case DialogMethod.select:
        return Wrap(
          spacing: tokens.spaceSm,
          runSpacing: tokens.spaceSm,
          children: [
            for (final option in request.options)
              OutlinedButton(
                onPressed: () => _answer(value: option),
                child: Text(option),
              ),
          ],
        );
      case DialogMethod.confirm:
        return Row(
          children: [
            OutlinedButton(
              onPressed: () => _answer(confirmed: false),
              child: Text(context.l10n.dialogDeny),
            ),
            SizedBox(width: tokens.spaceSm),
            FilledButton(
              onPressed: () => _answer(confirmed: true),
              child: Text(context.l10n.dialogApprove),
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
                  hintText:
                      request.placeholder ?? context.l10n.dialogAnswerHint,
                ),
                onSubmitted: (value) => _answer(value: value),
              ),
            ),
            SizedBox(width: tokens.spaceSm),
            FilledButton(
              onPressed: () => _answer(value: _input.text),
              child: Text(context.l10n.dialogSend),
            ),
          ],
        );
      case DialogMethod.editor:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(controller: _input, maxLines: 6),
            SizedBox(height: tokens.spaceSm),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton(
                onPressed: () => _answer(value: _input.text),
                child: Text(context.l10n.dialogSave),
              ),
            ),
          ],
        );
    }
  }
}
