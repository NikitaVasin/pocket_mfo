import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';

import 'pocket_mfo_test.dart' show Analytics, Backend;

class LaunchDriver implements MessagingDriver {
  final stream = StreamController<PushAction>.broadcast(sync: true);
  int starts = 0;
  int permissionRequests = 0;
  bool disposed = false;
  @override
  Stream<PushAction> get actions => stream.stream;
  @override
  Future<void> initialize(String key) async {
    starts++;
    stream.add(
      PushAction(
        id: 'launch',
        source: PushSource.appMetrica,
        data: {'type': 'route', 'url': '/orders'},
      ),
    );
  }

  @override
  Future<NotificationPermission> getPermission() async =>
      NotificationPermission.denied;
  @override
  Future<NotificationPermission> requestPermission() async {
    permissionRequests++;
    return NotificationPermission.denied;
  }

  @override
  Future<String?> getFcmToken() async => null;
  @override
  Future<void> setUserProfileId(String? id) async {}
  @override
  Future<void> dispose() async {
    disposed = true;
    await stream.close();
  }
}

void main() {
  for (final automatic in [true, false]) {
    testWidgets('startup permission enabled=$automatic is forwarded once', (
      tester,
    ) async {
      final driver = LaunchDriver();
      final navigator = GlobalKey<NavigatorState>();
      await tester.pumpWidget(
        MaterialApp(navigatorKey: navigator, home: const Text('Ready')),
      );
      final app = PocketMfo(
        pocketBase: Backend().client(),
        authCollection: 'users',
        appMetricaConfig: const AppMetricaConfig('test'),
        analytics: Analytics(),
        storage: MemorySessionStorage(),
        push: PocketMfoPushConfig(
          navigatorKey: navigator,
          onNavigate: (_) async {},
          messagingDriver: driver,
          requestPermissionOnStart: automatic,
        ),
      );
      addTearDown(app.dispose);
      await Future.wait([app.initialize(), app.initialize()]);
      await app.initialize();
      expect(driver.starts, 1);
      expect(driver.permissionRequests, automatic ? 1 : 0);
      expect(
        await app.messaging!.getPermission(),
        NotificationPermission.denied,
      );
      await tester.pumpAndSettle();
    });
  }

  testWidgets('cold push waits for guest auth and root Navigator', (
    tester,
  ) async {
    final backend = Backend();
    final analytics = Analytics();
    final driver = LaunchDriver();
    final navigator = GlobalKey<NavigatorState>();
    final opened = <String>[];
    final errors = <Object>[];
    final app = PocketMfo(
      pocketBase: backend.client(),
      authCollection: 'users',
      appMetricaConfig: const AppMetricaConfig('test'),
      onError: (error, _) => errors.add(error),
      analytics: analytics,
      storage: MemorySessionStorage(),
      push: PocketMfoPushConfig(
        navigatorKey: navigator,
        messagingDriver: driver,
        onNavigate: (uri) async {
          expectSync(analytics.id, isNotNull);
          expectSync(navigator.currentState, isNotNull);
          opened.add(uri.path);
        },
      ),
    );
    final initializing = app.initialize();
    await tester.pump();
    await initializing;
    expect(backend.creates, 1);
    expect(opened, isEmpty);
    await tester.pumpWidget(
      MaterialApp(navigatorKey: navigator, home: const Text('Ready')),
    );
    await tester.pump();
    await tester.pumpAndSettle();
    expect(errors, isEmpty);
    expect(opened, ['/orders']);
    await app.initialize();
    expect(driver.starts, 1);
    expect(analytics.activations, 1);
    addTearDown(() async {
      await app.dispose();
      expect(driver.disposed, isTrue);
    });
  });
}
