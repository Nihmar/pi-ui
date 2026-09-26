/// Lenient JSON readers.
///
/// The server is the one that must tolerate unknown fields (`AGENTS.md`,
/// schema-first): a client reads what it knows and leaves the rest alone. These
/// helpers are the single place where a dynamic payload is narrowed, so no
/// screen ever casts `Map<String, dynamic>` by hand.
library;

/// The object at [value], or null when it is not an object.
Map<String, dynamic>? asMap(Object? value) =>
    value is Map<String, dynamic> ? value : null;

/// The list at [value], or an empty list when it is not a list.
List<Object?> asList(Object? value) =>
    value is List ? List<Object?>.from(value) : const [];

/// The list of objects at [value], skipping entries that are not objects.
List<Map<String, dynamic>> asMapList(Object? value) => [
  for (final item in asList(value)) ?asMap(item),
];

/// The non-empty string at [value], or null.
String? optStr(Object? value) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  return null;
}

/// The string at [value], or [fallback] when it is missing or not a string.
String str(Object? value, {String fallback = ''}) =>
    value is String ? value : fallback;

/// The integer at [value], or [fallback].
int intOf(Object? value, {int fallback = 0}) =>
    value is num ? value.toInt() : fallback;

/// The integer at [value], or null when it is not a number.
int? optInt(Object? value) => value is num ? value.toInt() : null;

/// The number at [value], or [fallback].
double doubleOf(Object? value, {double fallback = 0}) =>
    value is num ? value.toDouble() : fallback;

/// True only when [value] is exactly `true`.
bool boolOf(Object? value) => value is bool && value;

/// An RFC3339 timestamp (or a Unix millisecond number) as local time, or null.
DateTime? timeOf(Object? value) {
  if (value is String) {
    return DateTime.tryParse(value)?.toLocal();
  }
  if (value is num) {
    return DateTime.fromMillisecondsSinceEpoch(value.toInt());
  }
  return null;
}
