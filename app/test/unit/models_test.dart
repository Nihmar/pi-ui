import 'package:flutter_test/flutter_test.dart';
import 'package:piui/core/api/models.dart';

void main() {
  group('ModelOption', () {
    test('reads a pi Model object', () {
      final model = ModelOption.fromJson({
        'id': 'qwen3-coder',
        'name': 'Qwen3 Coder',
        'provider': 'llama.cpp',
        'contextWindow': 128000,
        'reasoning': true,
        'api': 'openai-completions',
        'cost': {'input': 0, 'output': 0},
      });
      expect(model.id, 'qwen3-coder');
      expect(model.label, 'Qwen3 Coder');
      expect(model.key, 'llama.cpp/qwen3-coder');
      expect(model.contextWindow, 128000);
      expect(model.reasoning, isTrue);
    });

    test('falls back to the id and survives a sparse entry', () {
      final model = ModelOption.fromJson({'id': 'qwen', 'provider': 'local'});
      expect(model.label, 'qwen');
      expect(model.contextWindow, 0);
      expect(model.reasoning, isFalse);
      expect(ModelOption.fromJson(null).id, '');
    });

    test('reads the list out of a get_available_models response', () {
      final models = ModelOption.listFrom({
        'models': [
          {'id': 'a', 'provider': 'p1'},
          {'id': 'b', 'provider': 'p2', 'reasoning': true},
          'not an object',
        ],
      });
      expect(models.map((model) => model.id), ['a', 'b']);
      expect(models[1].reasoning, isTrue);
    });

    test('an empty or missing list is empty, not an error', () {
      expect(ModelOption.listFrom(null), isEmpty);
      expect(ModelOption.listFrom(const {}), isEmpty);
      expect(ModelOption.listFrom({'models': 'nope'}), isEmpty);
    });
  });
}
