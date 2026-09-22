import 'dart:async';

import 'package:appmetrica_plugin/appmetrica_plugin.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'push_action.dart';

/// Platform boundary, injectable for tests. Implementations emit taps only.
abstract interface class MessagingDriver {
  Stream<PushAction> get actions;
  Future<void> initialize(String appMetricaApiKey);
  Future<NotificationPermission> requestPermission();
  Future<NotificationPermission> getPermission();
  Future<String?> getFcmToken();
  Future<void> setUserProfileId(String? id);
  Future<void> dispose();
}

final class NativeMessagingDriver implements MessagingDriver {
  NativeMessagingDriver({this.analyticsAlreadyActivated = false});

  /// The application integration owns activation and profile changes.
  final bool analyticsAlreadyActivated;
  static const _channel = MethodChannel('dev.appbase/app_messaging');
  static NativeMessagingDriver? _owner;
  final _actions = StreamController<PushAction>.broadcast();
  StreamSubscription<RemoteMessage>? _fcmClicks;
  bool _disposed = false;
  bool _nativeActivated = false;
  @override
  Stream<PushAction> get actions => _actions.stream;

  @override
  Future<void> initialize(String appMetricaApiKey) async {
    _ensureAlive();
    if (_owner != null && _owner != this) {
      throw StateError('Use one AppMessaging instance per application');
    }
    if (kIsWeb ||
        ![
          TargetPlatform.android,
          TargetPlatform.iOS,
        ].contains(defaultTargetPlatform)) {
      throw UnsupportedError('AppMessaging supports Android and iOS');
    }
    _owner = this;
    if (Firebase.apps.isEmpty) await Firebase.initializeApp();
    _ensureAlive();
    if (!analyticsAlreadyActivated) {
      await AppMetrica.activate(AppMetricaConfig(appMetricaApiKey));
    }
    _ensureAlive();
    // Subscribe before draining the native cold-start queue.
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'action')
        _nativeAction(Map<String, dynamic>.from(call.arguments as Map));
    });
    await _fcmClicks?.cancel();
    _ensureAlive();
    _fcmClicks = FirebaseMessaging.onMessageOpenedApp.listen(
      _firebaseAction,
      onError: _actions.addError,
    );
    _nativeActivated = true;
    final launchActions =
        await _channel.invokeListMethod<dynamic>('activate') ?? [];
    _ensureAlive();
    for (final action in launchActions) {
      _nativeAction(Map<String, dynamic>.from(action as Map));
    }
    final initial = await FirebaseMessaging.instance.getInitialMessage();
    _ensureAlive();
    if (initial != null) _firebaseAction(initial);
    await FirebaseMessaging.instance
        .setForegroundNotificationPresentationOptions(
          alert: true,
          badge: true,
          sound: true,
        );
  }

  void _nativeAction(Map<String, dynamic> raw) {
    if (_disposed) return;
    try {
      _actions.add(
        PushAction.fromPayload(
          id: raw['id'] as String,
          source: PushSource.appMetrica,
          payload: raw['payload'] as String,
        ),
      );
    } catch (error, stack) {
      _actions.addError(error, stack);
    }
  }

  void _firebaseAction(RemoteMessage message) {
    if (_disposed) return;
    // AppMetrica notifications have their own tap handler and delivery reports.
    if (message.data.containsKey('yamp') || message.data.containsKey('ymp'))
      return;
    try {
      final payload = message.data['payload'];
      final id = message.messageId ?? message.data['actionId'] as String?;
      if (id == null)
        throw const FormatException('FCM action needs messageId or actionId');
      _actions.add(
        payload is String
            ? PushAction.fromPayload(
                id: id,
                source: PushSource.firebase,
                payload: payload,
              )
            : PushAction(
                id: id,
                source: PushSource.firebase,
                data: message.data,
              ),
      );
    } catch (error, stack) {
      _actions.addError(error, stack);
    }
  }

  NotificationPermission _permission(NotificationSettings value) =>
      switch (value.authorizationStatus) {
        AuthorizationStatus.notDetermined =>
          NotificationPermission.notDetermined,
        AuthorizationStatus.denied => NotificationPermission.denied,
        AuthorizationStatus.deniedPermanently =>
          NotificationPermission.deniedPermanently,
        AuthorizationStatus.authorized => NotificationPermission.authorized,
        AuthorizationStatus.provisional => NotificationPermission.provisional,
      };

  @override
  Future<NotificationPermission> requestPermission() async =>
      _permission(await FirebaseMessaging.instance.requestPermission());
  @override
  Future<NotificationPermission> getPermission() async =>
      _permission(await FirebaseMessaging.instance.getNotificationSettings());
  @override
  Future<String?> getFcmToken() async {
    if (defaultTargetPlatform == TargetPlatform.iOS &&
        await FirebaseMessaging.instance.getAPNSToken() == null)
      return null;
    return FirebaseMessaging.instance.getToken();
  }

  @override
  Future<void> setUserProfileId(String? id) => AppMetrica.setUserProfileID(id);
  @override
  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await _fcmClicks?.cancel();
    if (_owner == this) {
      _channel.setMethodCallHandler(null);
      if (_nativeActivated) await _channel.invokeMethod<void>('detach');
      _owner = null;
    }
    await _actions.close();
  }

  void _ensureAlive() {
    if (_disposed) throw StateError('Messaging driver is disposed');
  }
}
