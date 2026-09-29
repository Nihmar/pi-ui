import 'errors.dart';

/// The Crockford base32 alphabet the server mints pairing codes from.
///
/// It has no `I`, `L`, `O` or `U`: the letters a person reads as `1`/`0`, and `U`
/// so a code never accidentally spells a word.
///
/// **This file mirrors the server.** [pairingCodeAlphabet], [pairingCodeLength]
/// and [normalizePairingCode] must stay byte-for-byte identical to
/// `server/internal/auth/invite.go` (`inviteAlphabet`, `inviteCodeLength`,
/// `NormalizeCode`): a client that normalizes differently from the server turns a
/// valid code into a rejected one. Change both sides in the same commit.
const pairingCodeAlphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/// How many characters a pairing code has (`4K9M27`).
const pairingCodeLength = 6;

/// The scheme of every pairing link (`piui://pair…`).
const pairingLinkScheme = 'piui';

/// The host of every pairing link (`piui://pair…`).
const pairingLinkHost = 'pair';

/// The link version this client understands.
///
/// A link whose `v` is anything else was minted by a newer app whose flow this
/// build cannot follow, so it is a user-facing "update the app" error rather
/// than a generic parse failure.
const pairingLinkVersion = '1';

/// The `error.message` values [parsePairingLink] throws.
///
/// They are keys, not sentences: the pairing screen turns them into a localized
/// message ([PairingLinkErrorKeys.invalid] → `pairLinkInvalid`,
/// [PairingLinkErrorKeys.version] → `pairLinkVersion`), so a parse failure is
/// never a hardcoded English string.
abstract final class PairingLinkErrorKeys {
  /// The text is not a `piui://pair` link, or it is missing its origin or code.
  static const invalid = 'pairLinkInvalid';

  /// The link was minted by a newer app (`v` this client does not know).
  static const version = 'pairLinkVersion';
}

/// A parsed `piui://pair` link: where the server is, the code to type and, when
/// the server terminates TLS, the certificate to pin.
///
/// A link the operator handed over is already a pinning decision, so a
/// [fingerprint] in it is trusted without asking again (see the pairing screen).
class PairingLink {
  const PairingLink({
    required this.origin,
    required this.code,
    this.secret,
    this.fingerprint,
  });

  /// The server origin, exactly as the link carried it (`http://pi-ui.local:8787`).
  final String origin;

  /// The pairing code, already normalized (`4K9M27`).
  final String code;

  /// The legacy QR secret, when an **old** link still carries one.
  ///
  /// It exists only so a new app can still pair against an old server whose `qr`
  /// invitation needs it; a current server ignores it (its QR carries the same
  /// code). It is forwarded, never stored.
  final String? secret;

  /// The TLS certificate fingerprint (`sha256`, lowercase hex) to pin, from the
  /// link's `fp` parameter. Null when the server terminates no TLS, or the link
  /// predates `fp`.
  final String? fingerprint;
}

/// Parses a `piui://pair` link into a [PairingLink].
///
/// Parsing is lenient: unknown parameters (a future `v=1` link with extra
/// fields) are ignored. Only a missing/foreign scheme or host, a missing `url`
/// or `code`, or an unknown `v` is a failure — it throws [PiuiException] whose
/// `message` is a [PairingLinkErrorKeys] value the screen localizes.
PairingLink parsePairingLink(String input) {
  final uri = Uri.tryParse(input.trim());
  if (uri == null ||
      uri.scheme.toLowerCase() != pairingLinkScheme ||
      uri.host.toLowerCase() != pairingLinkHost) {
    throw const PiuiException(
      ErrorCodes.badRequest,
      PairingLinkErrorKeys.invalid,
    );
  }
  // `queryParameters` decodes percent-escapes and keeps the last value of a
  // repeated key; anything the client does not know is simply never read.
  final params = uri.queryParameters;
  final version = params['v']?.trim();
  if (version != null && version.isNotEmpty && version != pairingLinkVersion) {
    throw const PiuiException(
      ErrorCodes.badRequest,
      PairingLinkErrorKeys.version,
    );
  }
  final origin = params['url']?.trim() ?? '';
  final code = normalizePairingCode(params['code'] ?? '');
  if (origin.isEmpty || code.isEmpty) {
    throw const PiuiException(
      ErrorCodes.badRequest,
      PairingLinkErrorKeys.invalid,
    );
  }
  return PairingLink(
    origin: origin,
    code: code,
    secret: _nonEmpty(params['secret']),
    fingerprint: _nonEmpty(params['fp'])?.toLowerCase(),
  );
}

/// Normalizes a pairing code the way the server does: trim whitespace, uppercase,
/// drop `-`/`_`/spaces, then map the letters a person confuses — `O`→`0`,
/// `I`→`1`, `L`→`1`.
///
/// The result is what goes on the wire, so `o1l-m27x` and `4k9 m27` become the
/// codes the server minted (`011M27X`, `4K9M27`).
String normalizePairingCode(String input) {
  final normalized = StringBuffer();
  for (final unit in input.trim().toUpperCase().runes) {
    final char = String.fromCharCode(unit);
    normalized.write(switch (char) {
      '-' || '_' || ' ' => '',
      'O' => '0',
      'I' || 'L' => '1',
      _ => char,
    });
  }
  return normalized.toString();
}

String? _nonEmpty(String? value) {
  final trimmed = value?.trim();
  return trimmed == null || trimmed.isEmpty ? null : trimmed;
}
