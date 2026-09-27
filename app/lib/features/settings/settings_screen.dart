import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/dto.dart';
import '../../core/api/errors.dart';
import '../../core/api/providers.dart';
import '../../core/api/settings.dart';
import '../../core/format.dart';
import '../../core/theme/theme_tokens.dart';

/// Settings: what this device is, what the server's policy says, and what is published.
///
/// Two audiences in one screen, because they are the same screen: any device reads the
/// policy and follows it (the theme is the server's), and an admin changes it. The server
/// enforces that split, so the screen only reflects it — an operator sees the values and a
/// sentence explaining who can change them.
class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tokens = context.tokens;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        actions: [
          IconButton(
            tooltip: 'Reload',
            onPressed: () {
              ref
                ..invalidate(serverSettingsProvider)
                ..invalidate(updatesProvider);
            },
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: ListView(
        padding: EdgeInsets.all(tokens.spaceMd),
        children: const [
          _DeviceSection(),
          SizedBox(height: 8),
          _PolicySection(),
          SizedBox(height: 8),
          _UpdatesSection(),
        ],
      ),
    );
  }
}

/// Who this client is and what it is allowed to do.
class _DeviceSection extends ConsumerWidget {
  const _DeviceSection();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profile = ref.watch(profileProvider).value;
    final status = ref.watch(connectionProvider).value;
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return _Section(
      title: 'This device',
      children: [
        _Row(label: 'Server', value: profile?.baseUrl ?? 'not paired'),
        _Row(
          label: 'Device',
          value: [
            profile?.deviceName,
            if (profile != null) profile.scope.name,
          ].whereType<String>().join(' · '),
        ),
        if (profile?.identity case final identity?) ...[
          _Row(label: 'Server version', value: identity.version),
          _Row(label: 'pi version', value: identity.piVersion),
          if (identity.features.isNotEmpty)
            _Row(label: 'Features', value: identity.features.join(', ')),
          if (identity.fingerprint case final fingerprint?)
            _Row(
              label: 'Certificate',
              value:
                  '${fingerprint.substring(0, fingerprint.length.clamp(0, 16))}…',
            ),
        ],
        _Row(
          label: 'Connection',
          value: status?.name ?? 'unknown',
          color: status?.isOnline == true ? tokens.success : tokens.warning,
        ),
        Padding(
          padding: EdgeInsets.only(top: tokens.spaceXs),
          child: Text(
            'The device token lives in the operating system keystore; the server keeps '
            'only its hash.',
            style: theme.textTheme.labelSmall,
          ),
        ),
      ],
    );
  }
}

/// The server's policy: values any device reads, only an admin writes.
class _PolicySection extends ConsumerWidget {
  const _PolicySection();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(serverSettingsProvider);
    final profile = ref.watch(profileProvider).value;
    final admin = profile?.scope.covers(DeviceScope.admin) ?? false;
    return _Section(
      title: 'Server policy',
      children: [
        if (settings.value?.isEmpty ?? true)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: Text(
              'This server has no settings: it was started without a state directory.',
            ),
          )
        else
          settings.when(
            loading: () => const Padding(
              padding: EdgeInsets.all(12),
              child: Center(child: CircularProgressIndicator()),
            ),
            error: (error, _) => Text('The settings could not be read: $error'),
            data: (value) => Column(
              children: [
                for (final definition in value.known)
                  _SettingRow(
                    definition: definition,
                    settings: value,
                    admin: admin,
                  ),
                if (!admin)
                  Padding(
                    padding: EdgeInsets.only(top: 8),
                    child: Text(
                      'Changing these needs an admin device; this one is a '
                      '${profile?.scope.name ?? 'viewer'}.',
                      style: Theme.of(context).textTheme.labelSmall,
                    ),
                  ),
              ],
            ),
          ),
      ],
    );
  }
}

/// One setting: enough UI to change it, and a way back to its default.
class _SettingRow extends ConsumerStatefulWidget {
  const _SettingRow({
    required this.definition,
    required this.settings,
    required this.admin,
  });

  final SettingDefinition definition;
  final ServerSettings settings;
  final bool admin;

  @override
  ConsumerState<_SettingRow> createState() => _SettingRowState();
}

class _SettingRowState extends ConsumerState<_SettingRow> {
  var _busy = false;
  String? _error;
  late final TextEditingController _text = TextEditingController(
    text: '${widget.settings.valueOf(widget.definition.key) ?? ''}',
  );

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _change(Object? value) async {
    final client = ref.read(clientProvider);
    if (client == null || _busy) {
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await client.patchSettings({widget.definition.key: value});
      ref.invalidate(serverSettingsProvider);
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) {
        setState(() => _busy = false);
      }
    }
  }

  Future<void> _reset() async {
    final client = ref.read(clientProvider);
    if (client == null) {
      return;
    }
    try {
      await client.resetSetting(widget.definition.key);
      ref.invalidate(serverSettingsProvider);
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final definition = widget.definition;
    final value = widget.settings.valueOf(definition.key);
    final changed = !widget.settings.isDefault(definition.key);
    return Padding(
      padding: EdgeInsets.symmetric(vertical: tokens.spaceXs),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(definition.key, style: theme.textTheme.bodyMedium),
                        if (changed) ...[
                          SizedBox(width: tokens.spaceXs),
                          Text(
                            'changed',
                            style: theme.textTheme.labelSmall?.copyWith(
                              color: tokens.accent,
                            ),
                          ),
                        ],
                      ],
                    ),
                    Text(
                      definition.description,
                      style: theme.textTheme.labelSmall,
                    ),
                  ],
                ),
              ),
              SizedBox(width: tokens.spaceSm),
              _editor(context, value),
              if (widget.admin && changed)
                IconButton(
                  tooltip: 'Back to the default',
                  onPressed: _reset,
                  icon: const Icon(Icons.settings_backup_restore, size: 18),
                ),
            ],
          ),
          if (_error != null)
            Text(
              _error!,
              style: theme.textTheme.labelSmall?.copyWith(color: tokens.error),
            ),
        ],
      ),
    );
  }

  /// The editor the catalogue's kind asks for: a switch is not a text field.
  Widget _editor(BuildContext context, Object? value) {
    final enabled = widget.admin && !_busy;
    return switch (widget.definition.kind) {
      'bool' => Switch(
        value: value == true,
        onChanged: enabled ? (next) => _change(next) : null,
      ),
      'enum' => DropdownButton<Object?>(
        value: value,
        onChanged: enabled ? (next) => _change(next) : null,
        items: [
          // The catalogue does not list the values of an enum, so the client offers what
          // the server currently holds plus its default: a value the server would refuse
          // must not be offered.
          for (final option in _enumOptions(value))
            DropdownMenuItem(value: option, child: Text('$option')),
        ],
      ),
      'int' => SizedBox(
        width: 96,
        child: TextFormField(
          initialValue: '$value',
          enabled: enabled,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(isDense: true),
          onFieldSubmitted: (text) => _change(int.tryParse(text)),
        ),
      ),
      _ => SizedBox(
        width: 200,
        child: TextFormField(
          controller: _text,
          enabled: enabled,
          decoration: const InputDecoration(isDense: true),
          onFieldSubmitted: _change,
        ),
      ),
    };
  }

  /// The options of an enum: what the catalogue allows, plus the current value when the
  /// server did not list it (an older server, a hand-edited database).
  List<Object?> _enumOptions(Object? value) {
    final options = <Object?>[...widget.definition.allowed];
    if (value != null && !options.contains(value)) {
      options.add(value);
    }
    if (options.isEmpty) {
      options.add(widget.settings.defaults[widget.definition.key]);
    }
    options.removeWhere((option) => option == null);
    return options;
  }
}

/// The update panel: versions, and the operator's script when the server owns it.
class _UpdatesSection extends ConsumerWidget {
  const _UpdatesSection();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profile = ref.watch(profileProvider).value;
    final admin = profile?.scope.covers(DeviceScope.admin) ?? false;
    final report = ref.watch(updatesProvider);
    final tokens = context.tokens;
    final theme = Theme.of(context);
    if (!admin) {
      return const SizedBox.shrink();
    }
    return _Section(
      title: 'Updates',
      children: [
        report.when(
          loading: () => const Padding(
            padding: EdgeInsets.all(12),
            child: Center(child: CircularProgressIndicator()),
          ),
          error: (error, _) => Text('The versions could not be read: $error'),
          data: (value) => Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final component in value.components)
                Padding(
                  padding: EdgeInsets.symmetric(vertical: tokens.spaceXs),
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              component.name,
                              style: theme.textTheme.bodyMedium,
                            ),
                            Text(
                              component.isUnknown
                                  ? 'installed ${component.current} · not checked (${component.error})'
                                  : 'installed ${component.current} · latest ${component.latest}',
                              style: theme.textTheme.labelSmall,
                            ),
                            if (component.checkedAt case final at?)
                              Text(
                                'checked ${relativeTime(at)}',
                                style: theme.textTheme.labelSmall,
                              ),
                          ],
                        ),
                      ),
                      if (component.updateAvailable)
                        Text(
                          'update',
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: tokens.accent,
                          ),
                        ),
                    ],
                  ),
                ),
              SizedBox(height: tokens.spaceSm),
              if (value.managed)
                Text(
                  'This deployment manages its own updates: the server runs no script of '
                  'its own.',
                  style: theme.textTheme.labelSmall,
                )
              else
                Align(
                  alignment: Alignment.centerLeft,
                  child: FilledButton.icon(
                    onPressed: () => _apply(context, ref),
                    icon: const Icon(Icons.system_update_alt, size: 16),
                    label: const Text('Run the update'),
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }

  /// Starts the operator's script as a task and says where its output goes.
  Future<void> _apply(BuildContext context, WidgetRef ref) async {
    final client = ref.read(clientProvider);
    if (client == null) {
      return;
    }
    final messenger = ScaffoldMessenger.of(context);
    try {
      final task = await client.applyUpdate(const []);
      final id = task['id'];
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            id is String
                ? 'Update started: task $id. Its output is on the tasks endpoint.'
                : 'Update started.',
          ),
        ),
      );
    } on PiuiException catch (error) {
      messenger.showSnackBar(SnackBar(content: Text(error.message)));
    }
  }
}

/// One titled block of the settings list.
class _Section extends StatelessWidget {
  const _Section({required this.title, required this.children});

  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: EdgeInsets.all(tokens.spaceMd),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: theme.textTheme.titleSmall),
            SizedBox(height: tokens.spaceSm),
            ...children,
          ],
        ),
      ),
    );
  }
}

/// One label/value line.
class _Row extends StatelessWidget {
  const _Row({required this.label, required this.value, this.color});

  final String label;
  final String value;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 140,
            child: Text(label, style: theme.textTheme.labelSmall),
          ),
          Expanded(
            child: SelectableText(
              value,
              style: theme.textTheme.bodySmall?.copyWith(color: color),
            ),
          ),
        ],
      ),
    );
  }
}
