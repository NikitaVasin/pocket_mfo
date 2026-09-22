import 'dart:async';
import 'dart:collection';

import 'messaging_driver.dart';
import 'push_action.dart';

typedef PushActionResolver = Future<void> Function(PushAction action);
typedef MessagingErrorHandler = void Function(Object error, StackTrace stack);

/// Own one instance for the application's lifetime. Initialize after mounting
/// the root navigator, or make [onAction] await the application's readiness.
final class AppMessaging {
  AppMessaging({MessagingDriver? driver})
    : _driver = driver ?? NativeMessagingDriver();

  final MessagingDriver _driver;
  final Queue<PushAction> _pending = Queue();
  final LinkedHashSet<String> _seen = LinkedHashSet();
  StreamSubscription<PushAction>? _subscription;
  Future<void>? _initialization;
  PushActionResolver? _resolve;
  MessagingErrorHandler? _onError;
  bool _draining = false;
  bool _ready = false;
  bool _disposed = false;

  Future<void> initialize({
    required String appMetricaApiKey,
    required PushActionResolver onAction,
    bool requestPermissionOnStart = false,
    MessagingErrorHandler? onError,
  }) {
    if (_disposed) throw StateError('AppMessaging is disposed');
    if (appMetricaApiKey.trim().isEmpty)
      throw ArgumentError.value(appMetricaApiKey, 'appMetricaApiKey');
    return _initialization ??= _initialize(
      appMetricaApiKey,
      onAction,
      requestPermissionOnStart,
      onError,
    );
  }

  Future<void> _initialize(
    String key,
    PushActionResolver resolver,
    bool request,
    MessagingErrorHandler? onError,
  ) async {
    _resolve = resolver;
    _onError = onError;
    _subscription = _driver.actions.listen(_enqueue, onError: _report);
    try {
      await _driver.initialize(key);
      if (_disposed) return;
      _ready = true;
      unawaited(_drain());
      if (request) await requestPermission();
    } catch (_) {
      await _subscription?.cancel();
      _subscription = null;
      _ready = false;
      _initialization = null;
      rethrow;
    }
  }

  Future<NotificationPermission> requestPermission() {
    _checkReady();
    return _driver.requestPermission();
  }

  Future<NotificationPermission> getPermission() {
    _checkReady();
    return _driver.getPermission();
  }

  /// Can be null until APNs registration completes on iOS. Token acquisition
  /// never blocks initialization; Firebase and the native bridge handle refresh.
  Future<String?> getFcmToken() {
    _checkReady();
    return _driver.getFcmToken();
  }

  Future<void> setUserProfileId(String? id) {
    _checkReady();
    return _driver.setUserProfileId(id);
  }

  void _checkReady() {
    if (!_ready || _disposed) throw StateError('Initialize AppMessaging first');
  }

  void _enqueue(PushAction action) {
    if (_disposed) return;
    final key = '${action.source.name}:${action.id}';
    if (!_seen.add(key)) return;
    if (_seen.length > 128) _seen.remove(_seen.first);
    _pending.add(action);
    if (_ready) unawaited(_drain());
  }

  Future<void> _drain() async {
    if (_draining || _disposed) return;
    _draining = true;
    try {
      while (_pending.isNotEmpty && !_disposed) {
        final action = _pending.removeFirst();
        try {
          await _resolve!(action);
        } catch (error, stack) {
          _report(error, stack);
        }
      }
    } finally {
      _draining = false;
    }
  }

  void _report(Object error, StackTrace stack) {
    if (_disposed) return;
    if (_onError case final handler?) {
      handler(error, stack);
    } else {
      Zone.current.handleUncaughtError(error, stack);
    }
  }

  Future<void> dispose() async {
    _disposed = true;
    _pending.clear();
    await _subscription?.cancel();
    await _driver.dispose();
  }
}
