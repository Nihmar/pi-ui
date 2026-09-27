import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/errors.dart';
import '../../core/api/profile.dart';
import '../../core/api/providers.dart';
import '../../core/l10n/l10n.dart';
import '../../core/router.dart';
import '../../core/theme/theme_tokens.dart';

/// Screen 1 of the mockup: point the app at the server that runs the sessions.
///
/// The URL is normalized (`pi-ui.local:8787` works), probed with `GET /health`
/// — which needs no credential — and then handed to the pairing screen.
class ConnectScreen extends ConsumerStatefulWidget {
  const ConnectScreen({super.key});

  @override
  ConsumerState<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends ConsumerState<ConnectScreen> {
  final _controller = TextEditingController();
  var _probing = false;
  var _reachable = false;
  String? _error;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  /// The URL the field holds, normalized, or null with the error shown.
  String? _url() {
    try {
      return normalizeServerUrl(_controller.text);
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
      return null;
    }
  }

  Future<void> _test() async {
    final baseUrl = _url();
    if (baseUrl == null) {
      return;
    }
    setState(() {
      _probing = true;
      _error = null;
      _reachable = false;
    });
    try {
      final healthy = await ref.read(healthProbeProvider)(baseUrl);
      if (!mounted) {
        return;
      }
      setState(() {
        _reachable = healthy;
        _error = healthy ? null : context.l10n.serverNotOk;
      });
    } on PiuiException catch (error) {
      if (!mounted) {
        return;
      }
      setState(() => _error = error.message);
    } finally {
      if (mounted) {
        setState(() => _probing = false);
      }
    }
  }

  void _continue() {
    final baseUrl = _url();
    if (baseUrl != null) {
      context.go(Routes.pairWith(baseUrl));
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(context.l10n.connectTitle)),
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.all(tokens.spaceLg),
          children: [
            SizedBox(height: tokens.spaceMd),
            Center(
              child: Column(
                children: [
                  Container(
                    width: 56,
                    height: 56,
                    decoration: BoxDecoration(
                      color: tokens.accent,
                      borderRadius: BorderRadius.circular(tokens.radiusLg),
                    ),
                    alignment: Alignment.center,
                    child: const Text(
                      'π',
                      style: TextStyle(
                        color: Colors.white,
                        fontSize: 26,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  SizedBox(height: tokens.spaceSm),
                  Text('pi-ui', style: theme.textTheme.titleMedium),
                  SizedBox(height: tokens.spaceXs),
                  Text(
                    context.l10n.connectIntro,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            SizedBox(height: tokens.spaceLg),
            TextField(
              controller: _controller,
              autofocus: true,
              keyboardType: TextInputType.url,
              autocorrect: false,
              onSubmitted: (_) => _continue(),
              decoration: InputDecoration(
                labelText: context.l10n.serverUrlLabel,
                hintText: context.l10n.serverUrlHint,
                errorText: _error,
                prefixIcon: const Icon(Icons.dns_outlined),
              ),
            ),
            SizedBox(height: tokens.spaceSm),
            if (_reachable)
              Align(
                alignment: Alignment.centerLeft,
                child: Chip(
                  avatar: Icon(Icons.check, size: 16, color: tokens.success),
                  label: Text(context.l10n.serverReachable),
                ),
              ),
            SizedBox(height: tokens.spaceMd),
            OutlinedButton.icon(
              onPressed: _probing ? null : _test,
              icon: _probing
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.wifi_tethering),
              label: Text(context.l10n.testConnection),
            ),
            SizedBox(height: tokens.spaceSm),
            FilledButton(
              onPressed: _continue,
              child: Text(context.l10n.continueToPairing),
            ),
            SizedBox(height: tokens.spaceLg),
            Text(context.l10n.connectHelp, style: theme.textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}
