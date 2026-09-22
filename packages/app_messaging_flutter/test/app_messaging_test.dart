import 'dart:async';

import 'package:app_messaging_flutter/app_messaging_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

class FakeDriver implements MessagingDriver {
  final events = StreamController<PushAction>.broadcast(sync: true);
  final started = Completer<void>();
  int permissions = 0;
  int activations = 0;
  PushAction? launchAction;
  @override
  Stream<PushAction> get actions => events.stream;
  @override
  Future<void> initialize(String key) async {
    activations++;
    if (launchAction case final action?) events.add(action);
    await started.future;
  }

  @override
  Future<NotificationPermission> requestPermission() async {
    permissions++;
    return NotificationPermission.denied;
  }

  @override
  Future<NotificationPermission> getPermission() async =>
      NotificationPermission.denied;
  @override
  Future<String?> getFcmToken() async => null;
  @override
  Future<void> setUserProfileId(String? id) async {}
  @override
  Future<void> dispose() => events.close();
}

PushAction tap(String id) => PushAction(
  id: id,
  source: PushSource.appMetrica,
  data: {'type': 'route', 'url': '/orders'},
);

void main() {
  test(
    'cold tap waits for SDK readiness and duplicate tap is delivered once',
    () async {
      final driver = FakeDriver()..launchAction = tap('cold');
      final service = AppMessaging(driver: driver);
      final actions = <String>[];
      final init = service.initialize(
        appMetricaApiKey: 'key',
        onAction: (a) async {
          actions.add(a.id);
        },
      );
      expect(actions, isEmpty);
      driver.started.complete();
      await init;
      driver.events.add(tap('cold'));
      await Future<void>.delayed(Duration.zero);
      expect(actions, ['cold']);
      expect(driver.permissions, 0);
      await service.dispose();
    },
  );

  test('permission prompt is opt-in; repeated initialization does not prompt twice', () async {
    final driver = FakeDriver()..started.complete();
    final service = AppMessaging(driver: driver);
    for (var i = 0; i < 2; i++) {
      await service.initialize(
        appMetricaApiKey: 'key',
        onAction: (_) async {},
        requestPermissionOnStart: true,
      );
    }
    expect(driver.activations, 1);
    expect(driver.permissions, 1);
    expect(await service.requestPermission(), NotificationPermission.denied);
    expect(driver.permissions, 2);
    expect(await service.getFcmToken(), isNull);
    await service.dispose();
  });

  test('actions wait for resolver readiness, callback errors do not drop later taps', () async {
    final driver = FakeDriver()..started.complete();
    final service = AppMessaging(driver: driver);
    final gate = Completer<void>();
    final actions = <String>[];
    final errors = <Object>[];
    await service.initialize(
      appMetricaApiKey: 'key',
      onError: (e, _) => errors.add(e),
      onAction: (a) async {
        await gate.future;
        if (a.id == 'bad') throw StateError('Cannot open');
        actions.add(a.id);
      },
    );
    driver.events.add(tap('bad'));
    driver.events.add(tap('good'));
    expect(actions, isEmpty);
    gate.complete();
    await Future<void>.delayed(Duration.zero);
    expect(errors, hasLength(1));
    expect(actions, ['good']);
    await service.dispose();
  });

  test(
    'dispose drops pending navigation and rejects new initialization',
    () async {
      final driver = FakeDriver();
      final service = AppMessaging(driver: driver);
      final actions = <String>[];
      final init = service.initialize(
        appMetricaApiKey: 'key',
        onAction: (a) async {
          actions.add(a.id);
        },
      );
      driver.events.add(tap('pending'));
      await service.dispose();
      driver.started.complete();
      await init;
      expect(actions, isEmpty);
      expect(
        () =>
            service.initialize(appMetricaApiKey: 'key', onAction: (_) async {}),
        throwsStateError,
      );
    },
  );

  test('payload requires an object and retains arbitrary application data', () {
    final action = PushAction.fromPayload(
      id: 'id',
      source: PushSource.firebase,
      payload: '{"custom":42}',
    );
    expect(action.data['custom'], 42);
    expect(() => action.data['custom'] = 1, throwsUnsupportedError);
    expect(
      () => PushAction.fromPayload(
        id: 'id',
        source: PushSource.firebase,
        payload: '[]',
      ),
      throwsFormatException,
    );
  });
}
