import 'dart:convert';

/// A decoder for a byte stream that arrives in chunks.
///
/// A PTY read can end in the middle of a multi-byte character, and decoding each chunk on
/// its own turns that character into `U+FFFD` — visibly, in the middle of a word. This keeps
/// the incomplete tail and prepends it to the next chunk, so a character is decoded once its
/// bytes are all there.
///
/// The tail is bounded by the longest UTF-8 sequence (4 bytes): anything longer that does not
/// decode is genuinely malformed, and is shown as replacement characters rather than held
/// forever.
class Utf8Stream {
  Utf8Stream({this.maxTail = 4});

  /// How many trailing bytes may wait for the rest of a character.
  final int maxTail;

  final List<int> _tail = [];

  /// Bytes waiting for the rest of their character, for a test.
  int get pendingBytes => _tail.length;

  /// Decodes the next chunk and returns what is now readable.
  ///
  /// The returned text never ends in the middle of a character: an incomplete tail stays
  /// inside the stream until a later chunk completes it, or until it is clear that no later
  /// chunk can.
  String push(List<int> chunk) {
    if (chunk.isEmpty) {
      return '';
    }
    final bytes = [..._tail, ...chunk];
    _tail.clear();
    try {
      return utf8.decode(bytes);
    } on FormatException catch (error) {
      final start = _startOfBrokenSequence(bytes, error.offset);
      final text = utf8.decode(bytes.sublist(0, start), allowMalformed: true);
      final rest = bytes.sublist(start);
      if (_looksIncomplete(rest)) {
        _tail.addAll(rest);
        return text;
      }
      return text + utf8.decode(rest, allowMalformed: true);
    }
  }

  /// Where the sequence the decoder gave up on begins.
  ///
  /// The offset points *past* a truncated sequence rather than at it (`utf8.decode` on the
  /// first byte of "è" says "unfinished" with an offset equal to the length), so the start is
  /// found by walking back over the continuation bytes and, when the byte before them
  /// announces more bytes than have arrived, including it.
  static int _startOfBrokenSequence(List<int> bytes, int? offset) {
    var start = (offset ?? bytes.length).clamp(0, bytes.length);
    while (start > 0 && _isContinuation(bytes[start - 1])) {
      start--;
    }
    if (start > 0) {
      final lead = bytes[start - 1];
      if (_sequenceLength(lead) > bytes.length - (start - 1)) {
        start--;
      }
    }
    return start;
  }

  /// True for a UTF-8 continuation byte.
  static bool _isContinuation(int byte) => byte >= 0x80 && byte <= 0xbf;

  /// True when a malformed tail could still become a character: it is short enough to be an
  /// incomplete sequence, and its first byte announces a length the bytes have not reached.
  bool _looksIncomplete(List<int> rest) {
    if (rest.isEmpty || rest.length > maxTail) {
      return false;
    }
    final expected = _sequenceLength(rest.first);
    return expected > rest.length;
  }

  /// The length a UTF-8 sequence starting with [first] announces, or 0 when it is not a
  /// lead byte.
  static int _sequenceLength(int first) {
    if (first >= 0xf0 && first <= 0xf7) {
      return 4;
    }
    if (first >= 0xe0 && first <= 0xef) {
      return 3;
    }
    if (first >= 0xc2 && first <= 0xdf) {
      return 2;
    }
    return 0;
  }
}
