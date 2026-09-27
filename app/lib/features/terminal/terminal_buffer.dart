import 'dart:convert';

/// The scrollback of one terminal.
///
/// It keeps the tail of what the shell printed, byte-exact, and decodes it lazily:
/// a chunk may split a UTF-8 rune, so decoding each chunk on arrival would corrupt the
/// character that straddles two of them. Bytes in, one string out.
class TerminalBuffer {
  TerminalBuffer({this.maxBytes = 256 * 1024});

  /// How much of the tail is kept. The end of a build is what someone reads, which is why
  /// the oldest bytes are dropped first.
  final int maxBytes;

  final List<int> _bytes = [];
  var _dropped = false;

  /// Appends one chunk.
  void append(List<int> chunk) {
    if (chunk.isEmpty) {
      return;
    }
    _bytes.addAll(chunk);
    if (_bytes.length > maxBytes) {
      _bytes.removeRange(0, _bytes.length - maxBytes);
      _dropped = true;
    }
  }

  /// True when part of the output is gone.
  bool get isTruncated => _dropped;

  /// How much is kept.
  int get length => _bytes.length;

  /// The whole scrollback, decoded.
  ///
  /// Decoding replaces an incomplete trailing rune rather than throwing: a terminal that
  /// shows the rest of a line is more useful than one that stops at a boundary.
  String get text => const Utf8Decoder(allowMalformed: true).convert(_bytes);

  /// Forgets everything.
  void clear() {
    _bytes.clear();
    _dropped = false;
  }
}
