import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'dto.dart';
import 'errors.dart';

/// Everything the client needs to talk to one server, minus the token.
///
/// The token is a secret and lives in the OS keystore on its own (see
/// [ProfileStore]); this object is the part that is safe to keep in plain
/// preferences: where the server is, what it is, which device this is and the
/// TLS fingerprint the user confirmed.
class ServerProfile {
  const ServerProfile({
    required this.baseUrl,
    required this.deviceName,
    this.token,
    this.deviceId,
    this.scope = DeviceScope.viewer,
    this.identity,
  });

  /// Origin plus optional path prefix, without a trailing slash
  /// (`https://pi-ui.example/pi-ui`).
  final String baseUrl;

  /// The name this device introduced itself with.
  final String deviceName;

  /// The device token from pairing; null before the first pairing.
  final String? token;

  /// The public device id (`d_…`), when paired.
  final String? deviceId;

  /// What this device may do.
  final DeviceScope scope;

  /// What the server reported about itself.
  final ServerIdentity? identity;

  /// True once a token exists.
  bool get isPaired => (token ?? '').isNotEmpty;

  /// The REST base the client appends `/sessions` and friends to.
  Uri get apiBase => Uri.parse('$baseUrl/api/v1');

  /// The WebSocket endpoint of this server.
  Uri get wsUri {
    final origin = Uri.parse(baseUrl);
    return origin.replace(
      scheme: origin.scheme == 'https' ? 'wss' : 'ws',
      path: '${origin.path}/ws/v1',
    );
  }

  /// The host a user reads in the settings screen.
  String get host => Uri.parse(baseUrl).host;

  /// The fingerprint the user confirmed, when the server terminates TLS.
  String? get fingerprint => identity?.fingerprint;

  ServerProfile copyWith({
    String? baseUrl,
    String? deviceName,
    String? token,
    String? deviceId,
    DeviceScope? scope,
    ServerIdentity? identity,
  }) => ServerProfile(
    baseUrl: baseUrl ?? this.baseUrl,
    deviceName: deviceName ?? this.deviceName,
    token: token ?? this.token,
    deviceId: deviceId ?? this.deviceId,
    scope: scope ?? this.scope,
    identity: identity ?? this.identity,
  );

  /// The profile without its token, for storage in plain preferences.
  Map<String, dynamic> toJson() => {
    'baseUrl': baseUrl,
    'deviceName': deviceName,
    if (deviceId != null) 'deviceId': deviceId,
    'scope': scope.name,
    if (identity != null) 'server': identity!.toJson(),
  };

  /// Reads a profile written by [toJson].
  factory ServerProfile.fromJson(Map<String, dynamic> json) {
    final server = json['server'];
    return ServerProfile(
      baseUrl: normalizeServerUrl(json['baseUrl'] as String? ?? ''),
      deviceName: json['deviceName'] as String? ?? 'pi-ui',
      deviceId: json['deviceId'] as String?,
      scope: DeviceScope.parse(json['scope']),
      identity: server is Map<String, dynamic>
          ? ServerIdentity.fromJson(server)
          : null,
    );
  }
}

/// Where the token lives: the OS keystore, and nothing weaker.
abstract class TokenStore {
  Future<String?> read();

  Future<void> write(String token);

  Future<void> clear();
}

/// The token in `flutter_secure_storage` (Android Keystore, libsecret, DPAPI).
class SecureTokenStore implements TokenStore {
  SecureTokenStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();

  static const _key = 'piui.device-token';

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read() => _storage.read(key: _key);

  @override
  Future<void> write(String token) => _storage.write(key: _key, value: token);

  @override
  Future<void> clear() => _storage.delete(key: _key);
}

/// A token store a test can drive without a platform channel.
class MemoryTokenStore implements TokenStore {
  MemoryTokenStore([this._token]);

  String? _token;

  @override
  Future<String?> read() async => _token;

  @override
  Future<void> write(String token) async => _token = token;

  @override
  Future<void> clear() async => _token = null;
}

/// Where the server profile lives.
///
/// The token is split off into [TokenStore] so this store can be as dumb as
/// `shared_preferences` without ever writing a credential there.
abstract class ProfileStore {
  /// The stored profile (token included when it exists), or null when unpaired.
  Future<ServerProfile?> read();

  /// Writes the whole profile, token included.
  Future<void> write(ServerProfile profile);

  /// Forgets the token and the profile: the next launch starts at onboarding.
  Future<void> clear();

  /// Writes a rotated token, leaving the rest of the profile alone.
  Future<ServerProfile?> writeToken(String token);
}

/// The `shared_preferences` + keystore implementation.
class PrefsProfileStore implements ProfileStore {
  PrefsProfileStore({TokenStore? tokens, Future<SharedPreferences>? prefs})
    : tokens = tokens ?? SecureTokenStore(),
      _prefs = prefs ?? SharedPreferences.getInstance();

  static const _key = 'piui.profile';

  /// The keystore seam.
  final TokenStore tokens;

  final Future<SharedPreferences> _prefs;

  @override
  Future<ServerProfile?> read() async {
    final raw = (await _prefs).getString(_key);
    if (raw == null) {
      return null;
    }
    final token = await tokens.read();
    var profile = ServerProfile.fromJson(
      jsonDecode(raw) as Map<String, dynamic>,
    );
    if (token != null) {
      profile = profile.copyWith(token: token);
    }
    return profile;
  }

  @override
  Future<void> write(ServerProfile profile) async {
    await (await _prefs).setString(_key, jsonEncode(profile.toJson()));
    final token = profile.token;
    if (token == null || token.isEmpty) {
      await tokens.clear();
      return;
    }
    await tokens.write(token);
  }

  @override
  Future<void> clear() async {
    await (await _prefs).remove(_key);
    await tokens.clear();
  }

  @override
  Future<ServerProfile?> writeToken(String token) async {
    final profile = await read();
    if (profile == null) {
      return null;
    }
    final updated = profile.copyWith(token: token);
    await write(updated);
    return updated;
  }
}

/// A profile store a test can drive without platform channels.
class MemoryProfileStore implements ProfileStore {
  MemoryProfileStore({ServerProfile? profile, TokenStore? tokens})
    : current = profile,
      tokens = tokens ?? MemoryTokenStore(profile?.token);

  /// The profile as written, without awaiting: a test asserts on it directly.
  ServerProfile? current;

  /// The token store behind this profile store, so a test can inspect it.
  final TokenStore tokens;

  @override
  Future<ServerProfile?> read() async {
    final profile = current;
    if (profile == null) {
      return null;
    }
    final token = await tokens.read();
    return token == null ? profile : profile.copyWith(token: token);
  }

  @override
  Future<void> write(ServerProfile profile) async {
    current = profile;
    final token = profile.token;
    if (token == null || token.isEmpty) {
      await tokens.clear();
    } else {
      await tokens.write(token);
    }
  }

  @override
  Future<void> clear() async {
    current = null;
    await tokens.clear();
  }

  @override
  Future<ServerProfile?> writeToken(String token) async {
    final profile = current;
    if (profile == null) {
      return null;
    }
    await write(profile.copyWith(token: token));
    return current;
  }
}

/// Normalizes what a user typed into a base URL: `pi-ui.local:8787` becomes
/// `http://pi-ui.local:8787`, a trailing slash disappears, `ws`/`wss` are read as
/// `http`/`https`.
///
/// It throws [PiuiException] with `bad_request` when there is no host to talk to,
/// so the onboarding screen can show the reason next to the field.
String normalizeServerUrl(String input) {
  var text = input.trim();
  if (text.isEmpty) {
    throw const PiuiException(
      ErrorCodes.badRequest,
      'Enter the server address.',
    );
  }
  if (!text.contains('://')) {
    text = 'http://$text';
  }
  final uri = Uri.tryParse(
    text.replaceFirst('wss://', 'https://').replaceFirst('ws://', 'http://'),
  );
  if (uri == null || uri.host.isEmpty) {
    throw PiuiException(
      ErrorCodes.badRequest,
      'That is not a server address: "$input".',
    );
  }
  if (uri.scheme != 'http' && uri.scheme != 'https') {
    throw PiuiException(
      ErrorCodes.badRequest,
      'Only http and https servers can be paired: "$input".',
    );
  }
  final path = uri.path == '/' ? '' : uri.path.replaceAll(RegExp(r'/+$'), '');
  // Built by hand: `Uri.replace(query: '')` writes an empty `?`, and a base URL
  // is a prefix for `$baseUrl/api/v1`, not a URI with parts to preserve.
  final port = uri.hasPort ? ':${uri.port}' : '';
  return '${uri.scheme}://${uri.host}$port$path';
}
