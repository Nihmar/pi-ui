import 'dart:convert';
import 'dart:io';

/// A tiny HTTP server that speaks the two endpoints the onboarding needs:
/// `/health` for the probe and `/auth/pair` for the token.
class FakeServer {
  FakeServer._(this._server, this.baseUrl);

  static Future<FakeServer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final fake = FakeServer._(server, 'http://127.0.0.1:${server.port}');
    server.listen(fake._handle);
    return fake;
  }

  final HttpServer _server;
  final String baseUrl;

  /// Every pairing body the client sent, decoded.
  final requests = <Map<String, dynamic>>[];

  /// When true, pairing answers `401 unauthorized`, like a wrong code does.
  var rejectPairing = false;

  Future<void> _handle(HttpRequest request) async {
    final path = request.uri.path;
    // The real server answers GET /api/v1/health (docs/api-v1.md).
    if (path == '/health' || path == '/api/v1/health') {
      await _json(request, 200, {'status': 'ok'});
      return;
    }
    if (path == '/api/v1/sessions') {
      await _json(request, 200, {'sessions': <Object>[]});
      return;
    }
    if (path == '/api/v1/auth/refresh') {
      await _json(request, 200, {
        'token': 'd_test.rotated',
        'expiresAt': '2026-10-26T12:00:00.000Z',
      });
      return;
    }
    if (path == '/api/v1/auth/pair') {
      final body = await utf8.decoder.bind(request).join();
      final decoded = jsonDecode(body);
      if (decoded is Map<String, dynamic>) {
        requests.add(decoded);
      }
      if (rejectPairing) {
        await _json(request, 401, {
          'error': {'code': 'unauthorized', 'message': 'wrong or expired code'},
        });
        return;
      }
      await _json(request, 201, {
        'deviceId': 'd_test',
        'token': 'd_test.secret',
        'scope': 'operator',
        'expiresAt': '2026-10-26T12:00:00.000Z',
        'server': {
          'version': '0.1.0',
          'piVersion': '0.87.1',
          'protocol': 1,
          'features': ['sessions', 'replay'],
          'limits': {'maxSessions': 4},
        },
      });
      return;
    }
    await _json(request, 404, {
      'error': {'code': 'not_found', 'message': path},
    });
  }

  Future<void> _json(HttpRequest request, int status, Object body) async {
    request.response
      ..statusCode = status
      ..headers.contentType = ContentType.json
      ..write(jsonEncode(body));
    await request.response.close();
  }

  Future<void> stop() => _server.close(force: true);
}
