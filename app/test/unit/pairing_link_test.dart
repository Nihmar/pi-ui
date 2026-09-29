import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/pairing_link.dart';

void main() {
  group('normalizePairingCode', () {
    // These mirror `auth.NormalizeCode` (server/internal/auth/invite.go) step by
    // step: the two normalizers must agree or a valid code stops matching.
    //
    // NOTE: the plan wrote the first vector as `o1l-m27x → 01M27X`, but that drops
    // a symbol: folding `l → 1` on top of the literal `1` gives `011`, so the
    // server (and this client) produce `011M27X`. Mirroring the server wins over
    // the typo — anything else would send codes the server rejects.
    test('mirrors the server on the shared vectors', () {
      expect(normalizePairingCode('o1l-m27x'), '011M27X');
      expect(normalizePairingCode(' 4k9 m27 '), '4K9M27');
    });

    test('is case-insensitive and strips every separator', () {
      expect(normalizePairingCode('4k9m27'), '4K9M27');
      expect(normalizePairingCode('4K9_M27'), '4K9M27');
      expect(normalizePairingCode('4K9-M27'), '4K9M27');
      expect(normalizePairingCode('  O I L 0  '), '0110');
    });

    test('leaves an empty or blank code empty', () {
      expect(normalizePairingCode(''), '');
      expect(normalizePairingCode('   '), '');
    });
  });

  group('parsePairingLink', () {
    test('reads the origin, the normalized code and the fingerprint', () {
      final link = parsePairingLink(
        'piui://pair?v=1&url=https%3A%2F%2Fpi.example&code=4k9m27'
        '&fp=ABCDEF0123',
      );
      expect(link.origin, 'https://pi.example');
      expect(link.code, '4K9M27');
      expect(link.fingerprint, 'abcdef0123');
      expect(link.secret, isNull);
    });

    test('ignores unknown parameters (lenient parsing)', () {
      final link = parsePairingLink(
        'piui://pair?v=1&url=http%3A%2F%2Fpi.local%3A8787&code=4K9M27'
        '&utm_source=terminal&future=whatever',
      );
      expect(link.origin, 'http://pi.local:8787');
      expect(link.code, '4K9M27');
      expect(link.fingerprint, isNull);
    });

    test('accepts a link without a version', () {
      final link = parsePairingLink(
        'piui://pair?url=http%3A%2F%2Fpi.local&code=4K9M27',
      );
      expect(link.code, '4K9M27');
    });

    test('keeps the legacy secret when an old link carries one', () {
      final link = parsePairingLink(
        'piui://pair?v=1&url=http%3A%2F%2Fpi.local&code=4K9M27&secret=s3cr3t',
      );
      expect(link.secret, 's3cr3t');
      expect(link.code, '4K9M27');
    });

    test('a link from a newer app is an "update the app" error', () {
      expect(
        () => parsePairingLink(
          'piui://pair?v=2&url=http%3A%2F%2Fpi.local&code=4K9M27',
        ),
        throwsA(
          isA<PiuiException>()
              .having((error) => error.code, 'code', ErrorCodes.badRequest)
              .having(
                (error) => error.message,
                'message',
                PairingLinkErrorKeys.version,
              ),
        ),
      );
    });

    test('rejects what is not a complete piui://pair link', () {
      const rejected = [
        '',
        'hello',
        'https://pi.local',
        'piui://other?url=http%3A%2F%2Fpi.local&code=4K9M27',
        // Missing the origin.
        'piui://pair?v=1&code=4K9M27',
        // Missing the code.
        'piui://pair?v=1&url=http%3A%2F%2Fpi.local',
        // Missing both.
        'piui://pair?v=1',
      ];
      for (final raw in rejected) {
        expect(
          () => parsePairingLink(raw),
          throwsA(
            isA<PiuiException>().having(
              (error) => error.message,
              'message',
              PairingLinkErrorKeys.invalid,
            ),
          ),
          reason: 'should reject: "$raw"',
        );
      }
    });
  });
}
