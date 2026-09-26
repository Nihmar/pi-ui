/// Small formatting helpers shared by the screens.
library;

/// Renders [at] as the short relative time a chat or a list shows.
String relativeTime(DateTime at, {DateTime? now}) {
  final reference = now ?? DateTime.now();
  final difference = reference.difference(at);
  if (difference.inSeconds < 45) {
    return 'just now';
  }
  if (difference.inMinutes < 60) {
    return '${difference.inMinutes}m ago';
  }
  if (difference.inHours < 24) {
    return '${difference.inHours}h ago';
  }
  if (difference.inDays == 1) {
    return 'yesterday';
  }
  if (difference.inDays < 7) {
    return '${difference.inDays}d ago';
  }
  return '${at.day}/${at.month}/${at.year}';
}

/// Renders a duration the way a tool card shows it.
String shortDuration(Duration duration) {
  if (duration.inMilliseconds < 1000) {
    return '${duration.inMilliseconds} ms';
  }
  final seconds = duration.inMilliseconds / 1000;
  if (seconds < 60) {
    return '${seconds.toStringAsFixed(seconds < 10 ? 1 : 0)} s';
  }
  return '${duration.inMinutes}m ${duration.inSeconds % 60}s';
}

/// Renders a token count in the `12.4k` form a status bar has room for.
String shortTokens(int value) {
  if (value < 1000) {
    return '$value';
  }
  final thousands = value / 1000;
  return '${thousands.toStringAsFixed(thousands < 10 ? 1 : 0)}k';
}
