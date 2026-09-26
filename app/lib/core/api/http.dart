import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:dio/io.dart';

import 'errors.dart';
import 'json.dart';

/// The `HttpClient` the client and the WebSocket share.
///
/// When a fingerprint is known, a TLS server whose certificate does not match is
/// rejected before a single byte of the response is read: that is the pin the
/// pairing flow confirmed. Before the first pairing there is nothing to pin, so
/// the certificate is accepted (TOFU) and the fingerprint the server reports is
/// what gets stored — see docs/api-v1.md, "Pairing flow".
HttpClient piuiHttpClient({String? fingerprint, Duration? connectTimeout}) {
  final client = HttpClient()
    ..connectionTimeout = connectTimeout ?? const Duration(seconds: 10)
    ..idleTimeout = const Duration(seconds: 30)
    ..userAgent = 'pi-ui-client';
  final pinned = fingerprint?.toLowerCase();
  client.badCertificateCallback = (certificate, host, port) {
    final actual = fingerprintOf(certificate);
    if (pinned == null) {
      return true;
    }
    return actual == pinned;
  };
  return client;
}

/// The SHA-256 fingerprint of a certificate as lowercase hex.
///
/// `dart:io` exposes only SHA-1 on [X509Certificate], and the protocol pins the
/// SHA-256 of the leaf's DER (schemas/server.json, `SrvTlsInfo`), so the digest
/// is computed here over the exact bytes the certificate is encoded as.
String fingerprintOf(X509Certificate certificate) =>
    sha256.convert(certificate.der).toString();

/// Builds a Dio for one base URL, with the token attached to every request.
Dio createDio({
  required String baseUrl,
  String? token,
  String? fingerprint,
  HttpClient? httpClient,
  Duration? timeout,
}) {
  final dio = Dio(
    BaseOptions(
      baseUrl: '$baseUrl/api/v1',
      connectTimeout: timeout ?? const Duration(seconds: 10),
      receiveTimeout: timeout ?? const Duration(seconds: 30),
      sendTimeout: timeout ?? const Duration(seconds: 30),
      headers: {
        'Accept': 'application/json',
        'X-Piui-Protocol': '1',
        if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
      },
      // The server answers errors with the same JSON envelope as successes, so a
      // non-2xx must be read, not thrown away as an opaque transport failure.
      validateStatus: (status) => status != null && status < 600,
    ),
  );
  dio.httpClientAdapter = IOHttpClientAdapter(
    createHttpClient: () =>
        httpClient ?? piuiHttpClient(fingerprint: fingerprint),
  );
  return dio;
}

/// Turns one Dio response into a value or a [PiuiException].
///
/// Every REST call of this client goes through here, so the taxonomy is applied
/// in exactly one place.
T decoded<T>(
  Response<dynamic> response,
  T Function(Map<String, dynamic>) read,
) {
  final status = response.statusCode ?? 0;
  if (status >= 200 && status < 300) {
    final body = response.data;
    if (body is Map<String, dynamic>) {
      return read(body);
    }
    throw const PiuiException(
      ErrorCodes.badFrame,
      'The server answered with something that is not a JSON object.',
    );
  }
  final failure =
      errorFromBody(
        response.data,
        status: status,
        retryAfter: retryAfterOf(response.headers.value('retry-after')),
      ) ??
      PiuiException(
        status == 401 ? ErrorCodes.unauthorized : ErrorCodes.badRequest,
        'HTTP $status from ${response.requestOptions.uri}',
        status: status,
      );
  throw failure;
}

/// Maps a transport-level Dio failure onto the client taxonomy.
PiuiException transportError(Object error) {
  if (error is PiuiException) {
    return error;
  }
  if (error is DioException) {
    return switch (error.type) {
      DioExceptionType.connectionTimeout ||
      DioExceptionType.receiveTimeout ||
      DioExceptionType.sendTimeout => const PiuiException(
        ErrorCodes.timeout,
        'The server did not answer in time.',
      ),
      DioExceptionType.connectionError => PiuiException(
        ErrorCodes.unreachable,
        'Could not reach ${error.requestOptions.uri.host}: '
        '${_short(error.message ?? error.type.name)}',
      ),
      DioExceptionType.cancel => const PiuiException(
        ErrorCodes.offline,
        'The request was cancelled.',
      ),
      _ => PiuiException(
        error.response?.statusCode == 401
            ? ErrorCodes.unauthorized
            : ErrorCodes.unreachable,
        _short(error.message ?? '$error'),
      ),
    };
  }
  return PiuiException(ErrorCodes.unreachable, _short('$error'));
}

/// Runs [call] and never lets a raw transport exception escape.
Future<T> guarded<T>(Future<T> Function() call) async {
  try {
    return await call();
  } on PiuiException {
    rethrow;
  } catch (error) {
    throw transportError(error);
  }
}

/// The `sessions` list of `GET /sessions`.
List<Map<String, dynamic>> sessionsOf(Object? body) =>
    asMapList(asMap(body)?['sessions']);

String _short(String message) {
  final flat = message.replaceAll('\n', ' ').trim();
  return flat.length <= 160 ? flat : '${flat.substring(0, 157)}…';
}
