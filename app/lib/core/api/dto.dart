import '../models/chat_entry.dart';
import '../models/session.dart';
import 'json.dart';

/// What a paired device may do (docs/api-v1.md, "Scopes").
enum DeviceScope {
  viewer,
  operator,
  admin;

  /// Parses the scope, defaulting to the least privilege a server could grant.
  static DeviceScope parse(Object? value) => switch (value) {
    'admin' => DeviceScope.admin,
    'operator' => DeviceScope.operator,
    _ => DeviceScope.viewer,
  };

  /// True when this scope covers [required].
  bool covers(DeviceScope required) => index >= required.index;
}

/// The server's build and capabilities (`SrvServerIdentity`).
class ServerIdentity {
  const ServerIdentity({
    required this.version,
    required this.piVersion,
    required this.protocol,
    this.features = const [],
    this.maxSessions,
    this.pairingTtlSec,
    this.fingerprint,
  });

  /// The pi-ui server version.
  final String version;

  /// The pi version the sessions run on.
  final String piVersion;

  /// The REST/WS protocol version; `1` is what this client speaks.
  final int protocol;

  /// Capability flags: a client degrades when one is missing.
  final List<String> features;

  /// The configured session limit, when the server reports it.
  final int? maxSessions;

  /// The pairing invitation TTL, when the server reports it.
  final int? pairingTtlSec;

  /// The TLS certificate fingerprint (`sha256`, lowercase hex), for pinning.
  final String? fingerprint;

  /// True when the server announces [feature].
  bool has(String feature) => features.contains(feature);

  /// True when this build speaks the protocol version this client speaks.
  bool get isCompatible => protocol == protocolVersion;

  /// The protocol version this client implements.
  static const protocolVersion = 1;

  /// Reads an identity out of `GET /server` or a pairing response's `server`.
  factory ServerIdentity.fromJson(Map<String, dynamic> json) {
    final limits = asMap(json['limits']) ?? const {};
    final tls = asMap(json['tls']);
    return ServerIdentity(
      version: str(json['version'], fallback: 'unknown'),
      piVersion: str(json['piVersion'], fallback: 'unknown'),
      protocol: intOf(json['protocol'], fallback: 1),
      features: [
        for (final feature in asList(json['features']))
          if (feature is String && feature.isNotEmpty) feature,
      ],
      maxSessions: optInt(limits['maxSessions']),
      pairingTtlSec: optInt(limits['pairingTtlSec']),
      fingerprint: optStr(tls?['fingerprintSha256'])?.toLowerCase(),
    );
  }

  /// The same identity with a fingerprint the caller confirmed.
  ServerIdentity withFingerprint(String? value) => ServerIdentity(
    version: version,
    piVersion: piVersion,
    protocol: protocol,
    features: features,
    maxSessions: maxSessions,
    pairingTtlSec: pairingTtlSec,
    fingerprint: value,
  );

  /// The identity as it is stored next to a profile: the server's own shape, so
  /// [ServerIdentity.fromJson] reads back what this writes (limits nested under
  /// `limits`, TLS under `tls`).
  Map<String, dynamic> toJson() => {
    'version': version,
    'piVersion': piVersion,
    'protocol': protocol,
    'features': features,
    'limits': {
      if (maxSessions != null) 'maxSessions': maxSessions,
      if (pairingTtlSec != null) 'pairingTtlSec': pairingTtlSec,
    },
    if (fingerprint != null) 'tls': {'fingerprintSha256': fingerprint},
  };
}

/// The device token and identity a pairing handed back (`SrvPairResponse`).
class PairResult {
  const PairResult({
    required this.deviceId,
    required this.token,
    required this.scope,
    required this.server,
    this.expiresAt,
  });

  /// The public device id (`d_…`).
  final String deviceId;

  /// The device token, returned exactly once and stored in the OS keystore.
  final String token;

  /// What this device may do.
  final DeviceScope scope;

  /// The server this token belongs to.
  final ServerIdentity server;

  /// When the token expires.
  final DateTime? expiresAt;

  /// Reads a pairing or refresh response.
  factory PairResult.fromJson(Map<String, dynamic> json) => PairResult(
    deviceId: str(json['deviceId']),
    token: str(json['token']),
    scope: DeviceScope.parse(json['scope']),
    server: ServerIdentity.fromJson(asMap(json['server']) ?? const {}),
    expiresAt: timeOf(json['expiresAt']),
  );
}

/// One paired device (`SrvDevice`), as the admin surface sees it.
class DeviceInfo {
  const DeviceInfo({
    required this.id,
    required this.scope,
    this.name,
    this.platform,
    this.current = false,
    this.createdAt,
    this.lastSeenAt,
    this.expiresAt,
  });

  final String id;
  final DeviceScope scope;
  final String? name;
  final String? platform;

  /// True for the device the caller is authenticated as.
  final bool current;

  final DateTime? createdAt;
  final DateTime? lastSeenAt;
  final DateTime? expiresAt;

  /// The name to show: the device's own name, else its id.
  String get displayName => name ?? id;

  /// True when this device is the one making the call.
  static DeviceInfo fromJson(
    Map<String, dynamic> json, {
    bool current = false,
  }) => DeviceInfo(
    id: str(json['deviceId'], fallback: str(json['id'], fallback: 'd_?')),
    scope: DeviceScope.parse(json['scope']),
    name: optStr(json['name']) ?? optStr(json['deviceName']),
    platform: optStr(json['platform']),
    current: boolOf(json['current']) || current,
    createdAt: timeOf(json['createdAt']),
    lastSeenAt: timeOf(json['lastSeenAt']),
    expiresAt: timeOf(json['expiresAt']),
  );
}

/// Maps the server's session projection (`sessions.Info`) onto the UI model.
SessionModel sessionFromJson(Object? value) {
  final json = asMap(value) ?? const <String, dynamic>{};
  return SessionModel(
    id: str(json['id'], fallback: 's_?'),
    cwd: str(json['cwd']),
    name: optStr(json['name']),
    status: sessionStatusFrom(json['status']),
    createdAt: timeOf(json['createdAt']) ?? DateTime.now(),
    pid: optInt(json['pid']),
    piSessionId: optStr(json['piSessionId']),
    modelId: optStr(json['modelId']),
    provider: optStr(json['modelProvider']),
    thinkingLevel: optStr(json['thinkingLevel']),
    messageCount: intOf(json['messageCount']),
    contextTokens: intOf(json['contextTokens']),
    contextWindow: intOf(json['contextWindow']),
    costUsd: doubleOf(json['costUsd']),
    pendingMessages: intOf(json['pendingMessageCount']),
    lastEventAt: timeOf(json['lastEventAt']),
    exitCode: optInt(json['exitCode']),
  );
}

/// Maps a status string onto [SessionStatus], unknown values read as `exited`.
SessionStatus sessionStatusFrom(Object? value) => switch (value) {
  'spawning' => SessionStatus.spawning,
  'ready' => SessionStatus.ready,
  'streaming' => SessionStatus.streaming,
  'stopping' => SessionStatus.stopping,
  'crashed' => SessionStatus.crashed,
  _ => SessionStatus.exited,
};

/// Maps a dialog method string onto [DialogMethod].
DialogMethod dialogMethodFrom(Object? value) => switch (value) {
  'confirm' => DialogMethod.confirm,
  'input' => DialogMethod.input,
  'editor' => DialogMethod.editor,
  _ => DialogMethod.select,
};
