import 'json.dart';

/// One row of the server's settings catalogue.
///
/// The catalogue is the server's: it says which keys exist, what type each one has, its
/// default and what it does. A client renders what it is told instead of carrying its own
/// list, which is what lets the server add a setting without an app release.
class SettingDefinition {
  const SettingDefinition({
    required this.key,
    required this.kind,
    required this.description,
    this.defaultValue,
    this.allowed = const [],
  });

  /// The dotted key (`git.write`, `ui.theme`).
  final String key;

  /// `bool`, `int`, `duration`, `string` or `enum`.
  final String kind;

  /// What the setting does, in one sentence.
  final String description;

  /// The value in force when nothing is stored.
  final Object? defaultValue;

  /// The values an enum accepts, empty for every other kind.
  final List<String> allowed;

  /// Reads one catalogue row.
  factory SettingDefinition.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return SettingDefinition(
      key: str(json['key']),
      kind: str(json['kind'], fallback: 'string'),
      description: str(json['description']),
      defaultValue: json['default'],
      allowed: [
        for (final entry in asList(json['allowed']))
          if (entry is String) entry,
      ],
    );
  }
}

/// Everything `GET /settings` answers: the effective values, the defaults and the
/// catalogue.
class ServerSettings {
  const ServerSettings({
    this.values = const {},
    this.defaults = const {},
    this.known = const [],
  });

  /// The value in force per key, defaults included.
  final Map<String, Object?> values;

  /// The default per key, so a screen can offer "reset".
  final Map<String, Object?> defaults;

  /// The catalogue, in the order the server lists it.
  final List<SettingDefinition> known;

  /// True when the server has nothing to show (no state directory).
  bool get isEmpty => known.isEmpty && values.isEmpty;

  /// The value of one key, or its default.
  Object? valueOf(String key) => values[key] ?? defaults[key];

  /// True when [key] holds its default.
  bool isDefault(String key) => values[key] == defaults[key];

  /// Reads the whole answer.
  factory ServerSettings.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return ServerSettings(
      values: asMap(json['values']) ?? const {},
      defaults: asMap(json['defaults']) ?? const {},
      known: [
        for (final entry in asMapList(json['known']))
          SettingDefinition.fromJson(entry),
      ],
    );
  }
}

/// One component of the update panel.
class UpdateComponent {
  const UpdateComponent({
    required this.name,
    required this.current,
    this.source,
    this.latest,
    this.updateAvailable = false,
    this.error,
    this.checkedAt,
  });

  final String name;
  final String current;
  final String? source;
  final String? latest;
  final bool updateAvailable;

  /// Why a lookup failed, when it did: an offline server still lists what it runs.
  final String? error;
  final DateTime? checkedAt;

  /// True when the check could not answer.
  bool get isUnknown => error != null && latest == null;

  /// Reads one component row.
  factory UpdateComponent.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return UpdateComponent(
      name: str(json['name']),
      current: str(json['current']),
      source: optStr(json['source']),
      latest: optStr(json['latest']),
      updateAvailable: boolOf(json['updateAvailable']),
      error: optStr(json['error']),
      checkedAt: timeOf(json['checkedAt']),
    );
  }
}

/// The whole `GET /updates` answer.
class UpdateReport {
  const UpdateReport({this.components = const [], this.managed = true});

  final List<UpdateComponent> components;

  /// True when applying is not this server's job: a client hides the button instead of
  /// offering one that answers 409.
  final bool managed;

  /// True when at least one component has something newer.
  bool get hasUpdates =>
      components.any((component) => component.updateAvailable);

  /// Reads the report.
  factory UpdateReport.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return UpdateReport(
      components: [
        for (final entry in asMapList(json['components']))
          UpdateComponent.fromJson(entry),
      ],
      // A report that does not say reads as managed: assuming the server may apply updates
      // would offer a button that answers 409.
      managed: json.containsKey('managed') ? boolOf(json['managed']) : true,
    );
  }
}
