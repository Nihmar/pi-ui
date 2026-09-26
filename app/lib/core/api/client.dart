import 'package:dio/dio.dart';

import '../models/session.dart';
import 'dto.dart';
import 'errors.dart';
import 'files.dart';
import 'git.dart';
import 'http.dart';
import 'json.dart';
import 'profile.dart';

/// The REST half of the server API (`/api/v1`, docs/api-v1.md).
///
/// One instance talks to one server with one device token; the token is attached
/// by the Dio built in [createDio], so no call ever forgets it. Errors are always
/// [PiuiException] with a code from `schemas/core.json`.
class PiUiClient {
  PiUiClient({required this.profile, Dio? dio, this.onTokenRotated})
    : _dio =
          dio ??
          createDio(
            baseUrl: profile.baseUrl,
            token: profile.token,
            fingerprint: profile.fingerprint,
          );

  /// The server this client talks to.
  final ServerProfile profile;

  final Dio _dio;

  /// Called with a rotated token so the caller can persist it.
  final void Function(PairResult result)? onTokenRotated;

  /// GET /health — no credential, no state.
  Future<bool> health() => guarded(() async {
    final response = await _dio.get<dynamic>('/health');
    return decoded(response, (body) => str(body['status']) == 'ok');
  });

  /// GET /server — the build and the capabilities of the server.
  ///
  /// Reachable with `viewer`, which is what lets a bootstrap server identify
  /// itself before any device is paired.
  Future<ServerIdentity> identity() => guarded(() async {
    final response = await _dio.get<dynamic>('/server');
    return decoded(response, ServerIdentity.fromJson);
  });

  /// GET /sessions — every session, live or finished, in creation order.
  Future<List<SessionModel>> sessions() => guarded(() async {
    final response = await _dio.get<dynamic>('/sessions');
    return decoded(
      response,
      (body) => [for (final item in sessionsOf(body)) sessionFromJson(item)],
    );
  });

  /// GET /sessions/{id} — one session.
  Future<SessionModel> session(String id) => guarded(() async {
    final response = await _dio.get<dynamic>('/sessions/$id');
    return decoded(response, sessionFromJson);
  });

  /// POST /sessions — spawn one pi child in [cwd].
  Future<SessionModel> createSession({required String cwd, String? name}) =>
      guarded(() async {
        final response = await _dio.post<dynamic>(
          '/sessions',
          data: {
            'cwd': cwd,
            if (name != null && name.trim().isNotEmpty) 'name': name.trim(),
          },
        );
        return decoded(response, sessionFromJson);
      });

  /// POST /sessions/{id}/stop — stop one session gracefully.
  Future<SessionModel> stopSession(String id) => guarded(() async {
    final response = await _dio.post<dynamic>('/sessions/$id/stop');
    return decoded(response, sessionFromJson);
  });

  /// POST /auth/refresh — rotate this device's token.
  ///
  /// The new token is handed to [PiUiClient.onTokenRotated] so the store can
  /// persist it: the old one stops working as soon as the new one is written.
  Future<PairResult> refresh() => guarded(() async {
    final response = await _dio.post<dynamic>('/auth/refresh');
    final result = decoded(
      response,
      (body) => PairResult.fromJson({
        ...body,
        'deviceId': str(body['deviceId'], fallback: profile.deviceId ?? ''),
        'server': asMap(body['server'])?.isNotEmpty == true
            ? body['server']
            : profile.identity?.toJson(),
      }),
    );
    onTokenRotated?.call(result);
    return result;
  });

  /// GET /workspaces — the roots this device may browse.
  Future<List<WorkspaceRoot>> workspaces() => guarded(() async {
    final response = await _dio.get<dynamic>('/workspaces');
    return decoded(response, WorkspaceRoot.listFrom);
  });

  /// GET /fs/list — one directory inside a workspace.
  Future<List<FsEntry>> listDirectory(String path) => guarded(() async {
    final response = await _dio.get<dynamic>(
      '/fs/list',
      queryParameters: {'path': path},
    );
    return decoded(response, FsEntry.listFrom);
  });

  /// GET /fs/stat — one path's metadata.
  Future<FsEntry> statFile(String path) => guarded(() async {
    final response = await _dio.get<dynamic>(
      '/fs/stat',
      queryParameters: {'path': path},
    );
    return decoded(response, FsEntry.fromJson);
  });

  /// GET /files/read — the content of one file.
  Future<FileContent> readFile(String path, {int? maxBytes}) =>
      guarded(() async {
        final response = await _dio.get<dynamic>(
          '/files/read',
          queryParameters: {
            'path': path,
            if (maxBytes != null) 'maxBytes': '$maxBytes',
          },
        );
        return decoded(response, FileContent.fromJson);
      });

  /// GET /git/status — the working tree of one repository.
  Future<GitStatus> gitStatus(String dir) => guarded(() async {
    final response = await _dio.get<dynamic>(
      '/git/status',
      queryParameters: {'dir': dir},
    );
    return decoded(response, GitStatus.fromJson);
  });

  /// GET /git/log — the newest commits of one repository.
  Future<List<GitCommit>> gitLog(String dir, {int limit = 20}) =>
      guarded(() async {
        final response = await _dio.get<dynamic>(
          '/git/log',
          queryParameters: {'dir': dir, 'limit': '$limit'},
        );
        return decoded(response, GitCommit.listFrom);
      });

  /// GET /git/diff — a unified diff of one repository.
  Future<String> gitDiff(String dir, {String? path, bool staged = false}) =>
      guarded(() async {
        final response = await _dio.get<dynamic>(
          '/git/diff',
          queryParameters: {
            'dir': dir,
            'path': ?path,
            if (staged) 'staged': 'true',
          },
        );
        return decoded(response, (body) => str(body['diff']));
      });

  /// POST /git/stage — adds paths to the index (operator + the `git.write` setting).
  Future<void> gitStage(String dir, List<String> paths) => guarded(() async {
    final response = await _dio.post<dynamic>(
      '/git/stage',
      data: {'dir': dir, 'paths': paths},
    );
    if ((response.statusCode ?? 0) >= 300) {
      decoded(response, (body) => body);
    }
  });

  /// POST /git/commit — records the index.
  Future<GitCommit> gitCommit(String dir, String message) => guarded(() async {
    final response = await _dio.post<dynamic>(
      '/git/commit',
      data: {'dir': dir, 'message': message},
    );
    return decoded(response, GitCommit.fromJson);
  });

  /// GET /settings — the server's own policy, as the effective values.
  ///
  /// It is a read for any device (`viewer`): the app follows the theme the deployment
  /// chose, and only an admin changes it.
  Future<Map<String, dynamic>> settings() => guarded(() async {
    final response = await _dio.get<dynamic>('/settings');
    return decoded(
      response,
      (body) => asMap(body['values']) ?? const <String, dynamic>{},
    );
  });

  /// GET /auth/devices — the paired devices (admin scope).
  Future<List<DeviceInfo>> devices() => guarded(() async {
    final response = await _dio.get<dynamic>('/auth/devices');
    return decoded(
      response,
      (body) => [
        for (final item in asMapList(body['devices']))
          DeviceInfo.fromJson(item),
      ],
    );
  });

  /// DELETE /auth/devices/{id} — revoke one device (admin scope).
  Future<void> revokeDevice(String id) => guarded(() async {
    final response = await _dio.delete<dynamic>('/auth/devices/$id');
    if ((response.statusCode ?? 0) >= 300) {
      decoded(response, (body) => body);
    }
  });

  /// POST /auth/pair — pair this client with a server.
  ///
  /// Standalone on purpose: pairing has no token yet, so it builds its own Dio.
  /// A [code] is the typed invitation, a [secret] the QR one, and [password]
  /// the admin branch. Exactly one of them must be present.
  static Future<PairResult> pair({
    required String baseUrl,
    required String deviceName,
    String? code,
    String? secret,
    String? password,
    String? platform,
    String? fingerprint,
    Dio? dio,
  }) => guarded(() async {
    final client = dio ?? createDio(baseUrl: baseUrl, fingerprint: fingerprint);
    final response = await client.post<dynamic>(
      '/auth/pair',
      data: {
        'deviceName': deviceName,
        if (code != null && code.isNotEmpty) 'code': code.trim(),
        if (secret != null && secret.isNotEmpty) 'secret': secret,
        if (password != null && password.isNotEmpty) 'password': password,
        if (platform != null && platform.isNotEmpty) 'platform': platform,
      },
    );
    return decoded(response, PairResult.fromJson);
  });
}
