import 'dart:math';

/// Backoff exponencial + jitter para respuestas 429 (rate limit). El
/// servidor no manda `Retry-After` ni cabeceras `X-RateLimit-*`
/// (`internal/security/ratelimit.go`), así que el cliente decide su propio
/// backoff en vez de esperar una pista del servidor que nunca llega.
///
/// `delayFn` es inyectable para que los tests no tengan que esperar
/// segundos reales.
class RetryPolicy {
  RetryPolicy({
    this.maxAttempts = 3,
    List<Duration>? baseDelays,
    Random? random,
    Future<void> Function(Duration)? delayFn,
  })  : _baseDelays = baseDelays ??
            const [
              Duration(seconds: 1),
              Duration(seconds: 2),
              Duration(seconds: 4),
            ],
        _random = random ?? Random(),
        _delayFn = delayFn ?? ((d) => Future<void>.delayed(d));

  final int maxAttempts;
  final List<Duration> _baseDelays;
  final Random _random;
  final Future<void> Function(Duration) _delayFn;

  bool shouldRetry(int attempt) => attempt < maxAttempts;

  Future<void> waitBeforeRetry(int attempt) {
    final base = _baseDelays[min(attempt, _baseDelays.length - 1)];
    final jitter = Duration(milliseconds: _random.nextInt(250));
    return _delayFn(base + jitter);
  }
}
