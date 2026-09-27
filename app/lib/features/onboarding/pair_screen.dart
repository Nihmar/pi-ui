import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/providers.dart';
import '../../core/l10n/l10n.dart';
import '../../core/theme/theme_tokens.dart';

/// Screens 3 and 4 of the mockup: type the pairing code, and confirm the
/// certificate fingerprint when the server pinned one.
///
/// The code is the fallback for a device without a camera; the admin password is
/// the recovery branch for an installation whose devices are all gone.
class PairScreen extends ConsumerStatefulWidget {
  const PairScreen({super.key, required this.baseUrl});

  /// The server this pairing targets, already normalized by the previous screen.
  final String baseUrl;

  @override
  ConsumerState<PairScreen> createState() => _PairScreenState();
}

class _PairScreenState extends ConsumerState<PairScreen> {
  final _code = TextEditingController();
  final _device = TextEditingController();
  var _admin = false;
  final _password = TextEditingController();
  var _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _device.text = 'pi-ui ${currentPlatform()}';
  }

  @override
  void dispose() {
    _code.dispose();
    _device.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _pair() async {
    final code = _code.text.trim();
    final password = _password.text;
    final deviceName = _device.text.trim();
    if (deviceName.isEmpty) {
      setState(() => _error = 'Give this device a name.');
      return;
    }
    if (_admin ? password.isEmpty : code.isEmpty) {
      setState(
        () => _error = _admin
            ? 'Type the admin password.'
            : 'Type the pairing code the server printed.',
      );
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await ref
          .read(profileProvider.notifier)
          .pair(
            baseUrl: widget.baseUrl,
            deviceName: deviceName,
            code: _admin ? null : code,
            password: _admin ? password : null,
          );
      if (!mounted) {
        return;
      }
      final fingerprint = result.server.fingerprint;
      if (fingerprint != null) {
        // Which the user just pinned: confirm it before the profile is used, so
        // a self-signed certificate is a decision and not a surprise.
        final trusted = await showDialog<bool>(
          context: context,
          barrierDismissible: false,
          builder: (context) => _FingerprintDialog(
            host: Uri.parse(widget.baseUrl).host,
            fingerprint: fingerprint,
          ),
        );
        if (trusted != true) {
          await ref.read(profileProvider.notifier).logout();
          return;
        }
      }
      // The redirect in the router takes it from here.
    } on PiuiException catch (error) {
      setState(() {
        _error = error.isUnauthorized
            ? '${error.message} The code may be consumed, expired or wrong.'
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
    return Scaffold(
      appBar: AppBar(title: const Text('Pair with the server')),
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.all(tokens.spaceLg),
          children: [
            Container(
              padding: EdgeInsets.all(tokens.spaceMd),
              decoration: BoxDecoration(
                color: tokens.surface,
                borderRadius: BorderRadius.circular(tokens.radiusMd),
                border: Border.all(color: tokens.border),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(widget.baseUrl, style: theme.textTheme.titleSmall),
                  SizedBox(height: tokens.spaceXs),
                  Text(
                    'The invitation is single use and expires after ten minutes.',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            SizedBox(height: tokens.spaceLg),
            TextField(
              controller: _device,
              decoration: const InputDecoration(
                labelText: 'Device name',
                prefixIcon: Icon(Icons.phone_android),
              ),
            ),
            SizedBox(height: tokens.spaceMd),
            if (!_admin)
              TextField(
                controller: _code,
                autofocus: true,
                textCapitalization: TextCapitalization.characters,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 22,
                  letterSpacing: 6,
                ),
                decoration: const InputDecoration(
                  labelText: 'Pairing code',
                  hintText: '4K9M27',
                ),
                onSubmitted: (_) => _pair(),
              )
            else
              TextField(
                controller: _password,
                autofocus: true,
                obscureText: true,
                decoration: const InputDecoration(
                  labelText: 'Admin password',
                  prefixIcon: Icon(Icons.key_outlined),
                ),
                onSubmitted: (_) => _pair(),
              ),
            if (_error != null) ...[
              SizedBox(height: tokens.spaceSm),
              Text(
                _error!,
                style: theme.textTheme.bodySmall?.copyWith(color: tokens.error),
              ),
            ],
            SizedBox(height: tokens.spaceLg),
            FilledButton(
              onPressed: _busy ? null : _pair,
              child: _busy
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Pair'),
            ),
            SizedBox(height: tokens.spaceSm),
            TextButton(
              onPressed: _busy
                  ? null
                  : () => setState(() {
                      _admin = !_admin;
                      _error = null;
                    }),
              child: Text(
                _admin
                    ? 'Use a pairing code instead'
                    : 'Pair with the admin password',
              ),
            ),
            SizedBox(height: tokens.spaceMd),
            Text(
              _admin
                  ? 'The password branch mints an admin device: it manages '
                        'devices, settings, MCP and updates.'
                  : 'Pairing with a code grants operator: it drives sessions '
                        'but cannot manage the server.',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

/// Screen 4 of the mockup: the fingerprint is the only thing a user can verify.
class _FingerprintDialog extends StatelessWidget {
  const _FingerprintDialog({required this.host, required this.fingerprint});

  final String host;
  final String fingerprint;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final grouped = [
      for (var index = 0; index < fingerprint.length; index += 2)
        fingerprint.substring(index, (index + 2).clamp(0, fingerprint.length)),
    ].join(':').toUpperCase();
    return AlertDialog(
      title: Text(context.l10n.fingerprintTitle),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(host, style: Theme.of(context).textTheme.titleSmall),
          SizedBox(height: tokens.spaceSm),
          Text(
            context.l10n.fingerprintLabel,
            style: Theme.of(context).textTheme.bodySmall,
          ),
          SizedBox(height: tokens.spaceXs),
          SelectableText(
            grouped,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
          ),
          SizedBox(height: tokens.spaceMd),
          Text(
            context.l10n.fingerprintNote,
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: Text(context.l10n.cancel),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(context.l10n.fingerprintTrust),
        ),
      ],
    );
  }
}
