import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/models.dart';
import '../../../core/api/providers.dart';
import '../../../core/theme/breakpoints.dart';
import '../../../core/theme/theme_tokens.dart';

/// Picks the model of one session.
///
/// The list comes from pi (`get_available_models`) through the generic command
/// passthrough, and the choice goes back the same way (`set_model`): the client never
/// holds a model catalogue of its own, because pi's configuration is what decides.
Future<void> showModelPicker(
  BuildContext context,
  WidgetRef ref,
  String sessionId,
) {
  if (context.isExpanded) {
    return showDialog<void>(
      context: context,
      builder: (context) => Dialog(
        child: SizedBox(width: 520, child: ModelPicker(sessionId: sessionId)),
      ),
    );
  }
  return showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) => ModelPicker(sessionId: sessionId),
  );
}

/// The picker itself: what pi offers, what is selected, and the reasoning level.
class ModelPicker extends ConsumerStatefulWidget {
  const ModelPicker({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<ModelPicker> createState() => _ModelPickerState();
}

class _ModelPickerState extends ConsumerState<ModelPicker> {
  var _busy = false;
  String? _error;

  /// The reasoning levels pi documents; `xhigh` and `max` appear only for a model that
  /// supports them, which is why the list is filtered by the chosen model.
  static const _levels = [
    'off',
    'minimal',
    'low',
    'medium',
    'high',
    'xhigh',
    'max',
  ];

  Future<void> _choose(ModelOption model) async {
    final actions = ref.read(sessionActionsProvider);
    if (actions == null || _busy) {
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await actions.setModel(widget.sessionId, model);
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) {
        setState(() => _busy = false);
      }
    }
  }

  Future<void> _chooseLevel(String level) async {
    final actions = ref.read(sessionActionsProvider);
    if (actions == null) {
      return;
    }
    try {
      await actions.setThinkingLevel(widget.sessionId, level);
    } on PiuiException catch (error) {
      if (mounted) {
        setState(() => _error = error.message);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final models = ref.watch(sessionModelsProvider(widget.sessionId));
    final session = ref.watch(sessionProvider(widget.sessionId));
    return Padding(
      padding: EdgeInsets.fromLTRB(
        tokens.spaceLg,
        tokens.spaceSm,
        tokens.spaceLg,
        tokens.spaceLg,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Model', style: theme.textTheme.titleMedium),
          SizedBox(height: tokens.spaceXs),
          Text(
            'pi owns the catalogue: this list is whatever it is configured with.',
            style: theme.textTheme.bodySmall,
          ),
          SizedBox(height: tokens.spaceMd),
          if (_error != null) ...[
            Text(
              _error!,
              style: theme.textTheme.bodySmall?.copyWith(color: tokens.error),
            ),
            SizedBox(height: tokens.spaceSm),
          ],
          Flexible(
            child: models.when(
              loading: () => const Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: CircularProgressIndicator()),
              ),
              error: (error, _) => Padding(
                padding: EdgeInsets.symmetric(vertical: tokens.spaceMd),
                child: Text(
                  'The models could not be read: $error',
                  style: theme.textTheme.bodySmall,
                ),
              ),
              data: (list) {
                if (list.isEmpty) {
                  return Padding(
                    padding: EdgeInsets.symmetric(vertical: tokens.spaceMd),
                    child: Text(
                      'pi reported no model. Check its configuration on the server.',
                      style: theme.textTheme.bodySmall,
                    ),
                  );
                }
                return ListView(
                  shrinkWrap: true,
                  children: [
                    for (final model in list)
                      ListTile(
                        dense: true,
                        leading: Icon(
                          model.reasoning
                              ? Icons.psychology_outlined
                              : Icons.memory,
                          size: 18,
                        ),
                        title: Text(model.label),
                        subtitle: Text(
                          model.contextWindow > 0
                              ? '${model.provider} · ${model.contextWindow} tokens'
                              : model.provider,
                        ),
                        trailing: session?.modelId == model.id
                            ? Icon(Icons.check, size: 18, color: tokens.accent)
                            : null,
                        onTap: _busy ? null : () => _choose(model),
                      ),
                  ],
                );
              },
            ),
          ),
          SizedBox(height: tokens.spaceMd),
          Text('Reasoning', style: theme.textTheme.titleSmall),
          SizedBox(height: tokens.spaceXs),
          Wrap(
            spacing: tokens.spaceXs,
            runSpacing: tokens.spaceXs,
            children: [
              for (final level in _levels)
                ChoiceChip(
                  label: Text(level),
                  selected: session?.thinkingLevel == level,
                  onSelected: (_) => _chooseLevel(level),
                ),
            ],
          ),
        ],
      ),
    );
  }
}
