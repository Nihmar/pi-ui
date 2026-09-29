import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/api/errors.dart';
import '../../core/api/pairing_link.dart';
import '../../core/l10n/l10n.dart';
import '../../core/theme/theme_tokens.dart';

/// The Android QR scanner: reads the pairing card the server printed.
///
/// It is pushed by the pairing screen and pops the parsed [PairingLink], so that
/// screen never has to know the camera exists and the scanner never has to know
/// how pairing works. A denied camera or an unrelated QR is a state, not a dead
/// end: the fallback is popping back to typing the code the same card shows.
class ScanScreen extends ConsumerStatefulWidget {
  const ScanScreen({super.key});

  @override
  ConsumerState<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends ConsumerState<ScanScreen> {
  final _controller = MobileScannerController();

  /// What to show when the last QR was not a pi-ui pairing link.
  String? _error;

  /// The payload the current error is about: the same code is not re-reported
  /// while it stays in frame, but a different one is read again.
  String? _rejected;

  /// Set once a link is found so a second frame cannot pop twice.
  var _popped = false;

  @override
  void dispose() {
    unawaited(_controller.dispose());
    super.dispose();
  }

  void _onDetect(BarcodeCapture capture) {
    if (_popped || capture.barcodes.isEmpty) {
      return;
    }
    final raw = capture.barcodes.first.rawValue;
    if (raw == null || raw.isEmpty || raw == _rejected) {
      return;
    }
    try {
      final link = parsePairingLink(raw);
      _popped = true;
      Navigator.of(context).pop(link);
    } on PiuiException catch (error) {
      // It is not a pairing card: say so and keep the camera running, so the
      // next QR the user points at is read normally.
      if (!mounted) {
        return;
      }
      setState(() {
        _rejected = raw;
        _error = _localize(error);
      });
    }
  }

  String _localize(PiuiException error) => switch (error.message) {
    PairingLinkErrorKeys.invalid => context.l10n.pairScanInvalid,
    PairingLinkErrorKeys.version => context.l10n.pairLinkVersion,
    _ => error.message,
  };

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final l10n = context.l10n;
    return Scaffold(
      appBar: AppBar(title: Text(l10n.pairScanTitle)),
      body: SafeArea(
        child: Stack(
          children: [
            Positioned.fill(
              child: MobileScanner(
                controller: _controller,
                onDetect: _onDetect,
                errorBuilder: (context, error) => _CameraFallback(
                  denied:
                      error.errorCode ==
                      MobileScannerErrorCode.permissionDenied,
                ),
              ),
            ),
            if (_error != null)
              Positioned(
                left: tokens.spaceLg,
                right: tokens.spaceLg,
                bottom: tokens.spaceLg,
                child: _ErrorBanner(message: _error!),
              ),
          ],
        ),
      ),
    );
  }
}

/// What the camera area shows when it cannot run: the permission is denied or
/// the device has no usable camera. Both offer the typing fallback.
class _CameraFallback extends StatelessWidget {
  const _CameraFallback({required this.denied});

  final bool denied;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    final l10n = context.l10n;
    return Center(
      child: Padding(
        padding: EdgeInsets.all(tokens.spaceLg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              denied ? Icons.no_photography_outlined : Icons.error_outline,
              color: tokens.textMuted,
            ),
            SizedBox(height: tokens.spaceMd),
            Text(
              denied ? l10n.pairScanDenied : l10n.pairScanFailed,
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyMedium,
            ),
            SizedBox(height: tokens.spaceLg),
            FilledButton(
              onPressed: () => Navigator.of(context).pop(),
              child: Text(l10n.pairScanFallback),
            ),
          ],
        ),
      ),
    );
  }
}

/// The banner under the preview that reports a QR which is not a pairing link.
class _ErrorBanner extends StatelessWidget {
  const _ErrorBanner({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final tokens = context.tokens;
    final theme = Theme.of(context);
    return Container(
      padding: EdgeInsets.all(tokens.spaceMd),
      decoration: BoxDecoration(
        color: tokens.toolErrorBg,
        borderRadius: BorderRadius.circular(tokens.radiusMd),
        border: Border.all(color: tokens.error),
      ),
      child: Row(
        children: [
          Icon(Icons.error_outline, color: tokens.error),
          SizedBox(width: tokens.spaceSm),
          Expanded(child: Text(message, style: theme.textTheme.bodySmall)),
        ],
      ),
    );
  }
}
