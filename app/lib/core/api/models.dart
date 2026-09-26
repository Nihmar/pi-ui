import 'json.dart';

/// One model pi can switch to, as `get_available_models` reports it.
///
/// The shape is pi's Model object, read leniently: a client shows the provider, the id
/// and the window, and ignores the rest (a newer pi may add fields this app has never
/// heard of, which is exactly what `AGENTS.md` asks of a client).
class ModelOption {
  const ModelOption({
    required this.provider,
    required this.id,
    this.name,
    this.contextWindow = 0,
    this.reasoning = false,
  });

  /// The provider nickname (`llama.cpp`, `anthropic`, …); never a URL or a key.
  final String provider;

  /// The model id pi was configured with.
  final String id;

  /// The human name, when the provider has one.
  final String? name;

  /// The context window in tokens, 0 when the server does not report one.
  final int contextWindow;

  /// True when the model can reason, which is what makes a thinking level meaningful.
  final bool reasoning;

  /// What a picker shows.
  String get label => name ?? id;

  /// `provider · id`, the pair `set_model` needs.
  String get key => '$provider/$id';

  /// Reads one model out of a `get_available_models` entry.
  factory ModelOption.fromJson(Object? value) {
    final json = asMap(value) ?? const <String, dynamic>{};
    return ModelOption(
      provider: str(json['provider']),
      id: str(json['id']),
      name: optStr(json['name']),
      contextWindow: intOf(json['contextWindow']),
      reasoning: boolOf(json['reasoning']),
    );
  }

  /// Reads the `data` of a `get_available_models` response.
  static List<ModelOption> listFrom(Object? data) => [
    for (final entry in asMapList(asMap(data)?['models']))
      ModelOption.fromJson(entry),
  ];
}
