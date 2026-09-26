import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/errors.dart';
import 'package:piui/core/api/http.dart';

void main() {
  group('error envelope', () {
    test('reads the code and the message', () {
      final error = errorFromBody({
        'error': {'code': 'forbidden_scope', 'message': 'no'},
      }, status: 403);
      expect(error?.code, 'forbidden_scope');
      expect(error?.message, 'no');
      expect(error?.status, 403);
      expect(error?.isUnauthorized, isFalse);
    });

    test('falls back to the taxonomy sentence', () {
      final error = errorFromBody(
        {
          'error': {'code': 'rate_limited'},
        },
        status: 429,
        retryAfter: const Duration(seconds: 5),
      );
      expect(error?.message, ErrorCodes.describe('rate_limited'));
      expect(error?.retryAfter, const Duration(seconds: 5));
      expect(error?.isTransient, isTrue);
    });

    test('an answer without an envelope is not an error', () {
      expect(errorFromBody({'sessions': <Object>[]}), isNull);
      expect(errorFromBody(null), isNull);
    });

    test('Retry-After is seconds, or nothing', () {
      expect(retryAfterOf('12'), const Duration(seconds: 12));
      expect(retryAfterOf('0'), isNull);
      expect(retryAfterOf('soon'), isNull);
      expect(retryAfterOf(null), isNull);
    });
  });

  group('transport failures', () {
    test('a refused connection is unreachable', () {
      final error = transportError(
        DioException(
          requestOptions: RequestOptions(path: '/sessions'),
          type: DioExceptionType.connectionError,
          message: 'Connection refused',
        ),
      );
      expect(error.code, ErrorCodes.unreachable);
      expect(error.message, contains('Connection refused'));
    });

    test('a timeout is a timeout', () {
      final error = transportError(
        DioException(
          requestOptions: RequestOptions(path: '/sessions'),
          type: DioExceptionType.receiveTimeout,
        ),
      );
      expect(error.code, ErrorCodes.timeout);
    });

    test('a PiuiException passes through unchanged', () {
      const original = PiuiException(ErrorCodes.offline, 'gone');
      expect(transportError(original), same(original));
    });
  });
}
