import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/errors.dart';
import '../../core/api/pairing_link.dart';
import '../../core/api/profile.dart';
import '../../core/api/providers.dart';
import '../../core/l10n/l10n.dart';
import '../../core/theme/theme_tokens.dart';
import 'scan_screen.dart';

/// The one pairing screen: point the app at a server and prove it with a code.
///
/// A single field takes either the address (`pi-ui.local:8787`) or a
/// `piui://pair…` link the operator pasted; a link fills the code, pins the
/// certificate it carries and drops the address field to the link's origin. The
/// code field is the fallback for a device without a camera; Android adds a
/// **Scan QR** button that reads the same link out of the pairing card.
///
/// The admin password is the second credential, behind one toggle: it mints an
/// admin-scoped device and is the way back in when every device is gone.
///
/// The scanner is Android-only (`mobile_scanner`): on Linux/Windows a link is
/// pasted or the code typed, which is the desktop's own flow.
class PairScreen extends ConsumerStatefulWidget {
  const PairScreen({super.key});

  @override
  ConsumerState<PairScreen> createState() => _PairScreenState();
}

class _PairScreenState extends ConsumerState<PairScreen> {
  /// Field keys so a test (and the scanner fallback) can address them directly.
  static const addressKey = Key('pair-address');
  static const codeKey = Key('pair-code');
  static const passwordKey = Key('pair-password');
  static const deviceKey = Key('pair-device');

  final _address = TextEditingController();
  final _code = TextEditingController();
  final _password = TextEditingController();
  final _device = TextEditingController();

  /// The fingerprint a pasted/scanned link carried; null when there was none.
  String? _fingerprint;

  /// The legacy secret a pasted/scanned link carried; null otherwise.
  String? _secret;

  /// Whether the credential being entered is the admin password.
  var _admin = false;

  var _busy = false;
  var _probing = false;
  var _reachable = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _device.text = 'pi-ui ${currentPlatform()}';
  }

  @override
  void dispose() {
    _address.dispose();
    _code.dispose();
    _password.dispose();
    _device.dispose();
    super.dispose();
  }

  /// Fills the rest of the form from a parsed link.
  void _applyLink(PairingLink link) {
    setState(() {
      _address.text = link.origin;
      _code.text = link.code;
      _fingerprint = link.fingerprint;
      _secret = link.secret;
      _error = null;
    });
  }

  /// Pasting a whole `piui://pair…` link fills the code and pins the link's
  /// fingerprint. A link still being typed is left alone until it is complete.
  void _onAddressChanged(String value) {
    final trimmed = value.trim();
    if (trimmed.toLowerCase().startsWith('$pairingLinkScheme:')) {
      try {
        _applyLink(parsePairingLink(trimmed));
      } on PiuiException {
        // Not a whole link yet: wait for the paste to finish.
      }
      return;
    }
    // A hand-typed address carries no link, so it pins nothing: drop anything a
    // previous paste had left behind.
    if (_fingerprint != null || _secret != null) {
      setState(() {
        _fingerprint = null;
        _secret = null;
      });
    }
  }

  /// The base URL the form currently targets, or null with the reason shown.
  ///
  /// A bare link in the address field is parsed here too, so a link that was
  /// pasted without an `onChanged` (a prefilled field) still works.
  String? _resolveAddress() {
    final raw = _address.text.trim();
    if (raw.toLowerCase().startsWith('$pairingLinkScheme:')) {
      try {
        final link = parsePairingLink(raw);
        _applyLink(link);
        return normalizeServerUrl(link.origin);
      } on PiuiException catch (error) {
        setState(() => _error = _localize(error));
        return null;
      }
    }
    try {
      return normalizeServerUrl(raw);
    } on PiuiException catch (error) {
      setState(() => _error = error.message);
      return null;
    }
  }

  /// Turns a link-parse failure into the sentence the user reads.
  String _localize(PiuiException error) => switch (error.message) {
    PairingLinkErrorKeys.invalid => context.l10n.pairLinkInvalid,
    PairingLinkErrorKeys.version => context.l10n.pairLinkVersion,
    _ => error.message,
  };

  Future<void> _test() async {
    final baseUrl = _resolveAddress();
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

  Future<void> _scan() async {
    final link = await Navigator.of(context).push<PairingLink>(
      MaterialPageRoute(builder: (context) => const ScanScreen()),
    );
    if (link == null || !mounted) {
      return;
    }
    _applyLink(link);
  }

  Future<void> _pair() async {
    final baseUrl = _resolveAddress();
    if (baseUrl == null) {
      return;
    }
    final code = normalizePairingCode(_code.text);
    final password = _password.text;
    final deviceName = _device.text.trim();
    if (deviceName.isEmpty) {
      setState(() => _error = context.l10n.pairDeviceNameRequired);
      return;
    }
    if (_admin ? password.isEmpty : code.isEmpty) {
      setState(
        () => _error = _admin
            ? context.l10n.pairPasswordRequired
            : context.l10n.pairCodeRequired,
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
            baseUrl: baseUrl,
            deviceName: deviceName,
            code: _admin ? null : code,
            password: _admin ? password : null,
            // Forwarded only when a pasted/scanned link carried one, so an old
            // server's QR invitation still works; a current one ignores it.
            secret: _admin ? null : _secret,
            fingerprint: _fingerprint,
          );
      if (!mounted) {
        return;
      }
      final reported = result.server.fingerprint;
      if (_fingerprint != null) {
        // The link pinned a fingerprint: a server that reports the same one is
        // pre-trusted, and one that reports another (or none) is refused.
        if (reported != _fingerprint) {
          await ref.read(profileProvider.notifier).logout();
          if (!mounted) {
            return;
          }
          setState(() => _error = context.l10n.pairFingerprintMismatch);
          return;
        }
      } else if (reported != null) {
        // No link pinned it, so the user confirms the only thing they can verify.
        final trusted = await showDialog<bool>(
          context: context,
          barrierDismissible: false,
          builder: (context) => _FingerprintDialog(
            host: Uri.parse(baseUrl).host,
            fingerprint: reported,
          ),
        );
        if (trusted != true) {
          await ref.read(profileProvider.notifier).logout();
          return;
        }
      }
      // The router's redirect takes it from here once the profile is paired.
    } on PiuiException catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _error = error.isUnauthorized
            ? '${error.message} ${context.l10n.pairWrongCodeHint}'
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
    final l10n = context.l10n;
    // Only Android can scan: the plugin has no Linux/Windows implementation, so
    // those builds show the typing flow instead of a button that cannot work.
    final showScan = defaultTargetPlatform == TargetPlatform.android;
    return Scaffold(
      appBar: AppBar(title: Text(l10n.pairTitle)),
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.all(tokens.spaceLg),
          children: [
            Text(
              'π',
              textAlign: TextAlign.center,
              style: theme.textTheme.displaySmall?.copyWith(
                color: tokens.accent,
                fontWeight: FontWeight.w700,
              ),
            ),
            SizedBox(height: tokens.spaceSm),
            Text(
              l10n.pairIntro,
              textAlign: TextAlign.center,
              style: theme.textTheme.bodySmall,
            ),
            SizedBox(height: tokens.spaceLg),
            TextField(
              key: addressKey,
              controller: _address,
              autofocus: true,
              keyboardType: TextInputType.url,
              autocorrect: false,
              onChanged: _onAddressChanged,
              onSubmitted: (_) => _pair(),
              decoration: InputDecoration(
                labelText: l10n.pairAddressLabel,
                hintText: l10n.pairAddressHint,
                prefixIcon: const Icon(Icons.dns_outlined),
              ),
            ),
            SizedBox(height: tokens.spaceMd),
            if (_admin)
              TextField(
                key: passwordKey,
                controller: _password,
                autofocus: true,
                obscureText: true,
                decoration: InputDecoration(
                  labelText: l10n.pairPasswordLabel,
                  prefixIcon: const Icon(Icons.key_outlined),
                ),
                onSubmitted: (_) => _pair(),
              )
            else
              TextField(
                key: codeKey,
                controller: _code,
                autofocus: true,
                textCapitalization: TextCapitalization.characters,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 22,
                  letterSpacing: 6,
                ),
                decoration: InputDecoration(
                  labelText: l10n.pairCodeLabel,
                  hintText: l10n.pairCodeHint,
                  prefixIcon: const Icon(Icons.key_outlined),
                ),
                onSubmitted: (_) => _pair(),
              ),
            SizedBox(height: tokens.spaceMd),
            TextField(
              key: deviceKey,
              controller: _device,
              decoration: InputDecoration(
                labelText: l10n.pairDeviceNameLabel,
                prefixIcon: const Icon(Icons.phone_android),
              ),
            ),
            SizedBox(height: tokens.spaceMd),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: _probing ? null : _test,
                    icon: _probing
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.wifi_tethering),
                    label: Text(l10n.testConnection),
                  ),
                ),
                if (_reachable) ...[
                  SizedBox(width: tokens.spaceSm),
                  Chip(
                    avatar: Icon(Icons.check, size: 16, color: tokens.success),
                    label: Text(l10n.serverReachable),
                  ),
                ],
              ],
            ),
            if (showScan) ...[
              SizedBox(height: tokens.spaceSm),
              OutlinedButton.icon(
                onPressed: _busy ? null : _scan,
                icon: const Icon(Icons.qr_code_scanner),
                label: Text(l10n.pairScanQr),
              ),
            ],
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
                  : Text(l10n.pairSubmit),
            ),
            SizedBox(height: tokens.spaceSm),
            TextButton(
              onPressed: _busy
                  ? null
                  : () => setState(() {
                      _admin = !_admin;
                      _error = null;
                    }),
              child: Text(_admin ? l10n.pairUseCode : l10n.pairUsePassword),
            ),
            SizedBox(height: tokens.spaceMd),
            Text(
              _admin ? l10n.pairAdminNote : l10n.pairOperatorNote,
              style: theme.textTheme.bodySmall,
            ),
            SizedBox(height: tokens.spaceXs),
            Text(l10n.pairInvitationNote, style: theme.textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}

/// The confirmation for a certificate no link pinned: the fingerprint is the
/// only thing a user can verify with `pi-ui tls fingerprint` on the server.
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
