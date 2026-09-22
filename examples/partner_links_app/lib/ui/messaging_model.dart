import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:push_links_flutter/push_links_flutter.dart';

import 'demo_model.dart';

/// Application-specific authentication readiness and routes stay in the demo.
class MessagingModel extends ChangeNotifier {
  MessagingModel({
    required this.demo,
    required GlobalKey<NavigatorState> navigatorKey,
    required Future<void> Function(Uri) onNavigate,
  }) {
    resolver = PushLinkResolver.pocketBase(
      navigatorKey: navigatorKey,
      pocketBase: demo.repository.pb,
      onNavigate: onNavigate,
      beforeOpen: (action) async {
        if (action is PartnerPushAction ||
            action is AppRouteAction && action.uri.path == '/orders') {
          if (!demo.signedIn) {
            await _signedIn.future;
          }
          if (_disposed || !demo.signedIn) {
            throw StateError('Authentication no longer available');
          }
          await _profileSync;
        }
      },
    );
    demo.addListener(_accountChanged);
  }

  final DemoModel demo;
  final messaging = AppMessaging();
  late final PushLinkResolver resolver;
  Completer<void> _signedIn = Completer();
  Future<void> _profileSync = Future.value();
  String? _profile;
  bool _initialized = false;
  bool _disposed = false;
  bool get available =>
      !kIsWeb &&
      [
        TargetPlatform.android,
        TargetPlatform.iOS,
      ].contains(defaultTargetPlatform);
  bool get ready => _initialized;
  String status = 'Уведомления запускаются…';

  Future<void> initialize() async {
    if (!available) {
      status = 'Push-уведомления доступны на Android и iOS';
      notifyListeners();
      return;
    }
    try {
      await messaging.initialize(
        appMetricaApiKey: 'fbca87ec-97c5-4df4-b4aa-aed80630f2fa',
        onAction: resolver.call,
        requestPermissionOnStart: true,
        onError: (_, _) => _showError(),
      );
      if (_disposed) return;
      _initialized = true;
      _accountChanged();
      status = _permissionText(await messaging.getPermission());
      notifyListeners();
    } catch (_) {
      _showError();
    }
  }

  void _accountChanged() {
    final id = demo.signedIn ? demo.repository.pb.authStore.record?.id : null;
    if (_initialized && id != _profile) {
      _profile = id;
      _profileSync = _profileSync.then((_) => messaging.setUserProfileId(id));
      // Observe failures without replacing the future used by beforeOpen.
      unawaited(_profileSync.catchError((Object _) => _showError()));
    }
    if (demo.signedIn) {
      if (!_signedIn.isCompleted) _signedIn.complete();
    } else if (_signedIn.isCompleted) {
      _signedIn = Completer();
    }
  }

  Future<void> requestPermission() async {
    try {
      status = _permissionText(await messaging.requestPermission());
      notifyListeners();
    } catch (_) {
      _showError();
    }
  }

  Future<void> preview(Map<String, dynamic> data) async {
    try {
      await resolver(
        PushAction(id: 'preview', source: PushSource.appMetrica, data: data),
      );
    } catch (_) {
      _showError();
    }
  }

  String _permissionText(NotificationPermission value) => switch (value) {
    NotificationPermission.authorized => 'Уведомления разрешены',
    NotificationPermission.provisional => 'Уведомления доставляются тихо',
    NotificationPermission.notDetermined => 'Разрешение ещё не запрашивалось',
    NotificationPermission.denied || NotificationPermission.deniedPermanently =>
      'Уведомления отключены в настройках устройства',
  };
  void _showError() {
    status = 'Не удалось настроить уведомления или открыть действие';
    notifyListeners();
  }

  @override
  void notifyListeners() {
    if (!_disposed) super.notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    resolver.dispose();
    demo.removeListener(_accountChanged);
    if (!_signedIn.isCompleted) _signedIn.complete();
    if (available) unawaited(messaging.dispose());
    super.dispose();
  }
}
