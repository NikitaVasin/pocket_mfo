import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/services.dart';
// Exercise the real SDK facade and its native wire response, rather than
// overriding PushDevices.deviceId and bypassing the production identifier path.
// ignore: implementation_imports
import 'package:appmetrica_plugin/src/platform/pigeon/appmetrica_api_pigeon.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = BasicMessageChannel<Object?>(
    'dev.flutter.pigeon.appmetrica_plugin.AppMetricaPigeon.requestStartupParams',
    AppMetricaPigeon.codec,
  );
  for (final apiId in [
    '18446744073709551615',
    null,
    '0123456789abcdef0123456789abcdef',
  ]) {
    test('native registration uses API device hash: $apiId', () async {
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      var requested = false;
      messenger.setMockDecodedMessageHandler<Object?>(channel, (message) async {
        requested = true;
        expect(message, [
          ['appmetrica_device_id_hash'],
        ]);
        return [
          StartupParamsPigeon(
            result: StartupParamsResultPigeon(
              deviceId: '0123456789abcdef0123456789abcdef',
              deviceIdHash: apiId,
            ),
          ),
        ];
      });
      addTearDown(
        () => messenger.setMockDecodedMessageHandler<Object?>(channel, null),
      );
      final requests = <Map<String, dynamic>>[];
      final pb = PocketBase(
        'https://pb.example',
        httpClientFactory: () => MockClient((request) async {
          requests.add(jsonDecode(request.body) as Map<String, dynamic>);
          return http.Response('{}', 200);
        }),
      );
      pb.authStore.save('token', RecordModel({'id': 'user00000000001'}));
      final devices = PushDevices(
        pocketBase: pb,
        storage: MemorySessionStorage(),
      );
      addTearDown(devices.dispose);
      final sync = devices.sync(enabled: true, language: 'ru');
      if (apiId == null) {
        await expectLater(sync, throwsStateError);
        expect(requests, isEmpty);
      } else if (apiId.length == 32) {
        await expectLater(sync, throwsFormatException);
        expect(requests, isEmpty);
      } else {
        await sync;
        expect(requests.single['deviceId'], apiId);
      }
      expect(requested, isTrue);
    });
  }

  test('serializes permission independently of enabled and defaults to undetermined', () async {
    final bodies = <Map<String, dynamic>>[];
    final pb = PocketBase(
      'https://pb.example',
      httpClientFactory: () => MockClient((request) async {
        bodies.add(jsonDecode(request.body) as Map<String, dynamic>);
        return http.Response('{}', 200);
      }),
    );
    pb.authStore.save('token', RecordModel({'id': 'user00000000001'}));
    final devices = PushDevices(
      pocketBase: pb,
      storage: MemorySessionStorage(),
      deviceId: () async => '42',
    );
    addTearDown(devices.dispose);
    await devices.sync(enabled: true, language: 'ru');
    expect(bodies.last['notificationPermission'], 'notDetermined');
    for (final permission in NotificationPermission.values) {
      await devices.sync(
        enabled: false,
        language: 'ru',
        notificationPermission: permission,
      );
      expect(bodies.last['notificationPermission'], permission.name);
      expect(bodies.last['enabled'], false);
      expect(bodies.last['id'], bodies.first['id']);
    }
  });

  test('lost response and restart reuse installation credentials', () async {
    final storage = MemorySessionStorage();
    final requests = <Map<String, dynamic>>[];
    var fail = true;
    final pb = PocketBase(
      'https://pb.example',
      httpClientFactory: () => MockClient((request) async {
        requests.add(jsonDecode(request.body) as Map<String, dynamic>);
        if (fail) {
          fail = false;
          throw http.ClientException('lost response');
        }
        return http.Response(
          jsonEncode({'id': requests.last['id']}),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
    pb.authStore.save(
      'first-token',
      RecordModel({'id': 'user00000000001', 'collectionId': 'members'}),
    );
    var devices = PushDevices(
      pocketBase: pb,
      storage: storage,
      deviceId: () async => '12345678901234567890',
    );
    await expectLater(
      devices.sync(enabled: true, language: 'ru', platform: 'android'),
      throwsA(isA<ClientException>()),
    );
    devices.dispose();
    devices = PushDevices(
      pocketBase: pb,
      storage: storage,
      deviceId: () async => '12345678901234567890',
    );
    await devices.sync(enabled: true, language: 'ru', platform: 'android');
    expect(requests, hasLength(2));
    expect(requests[0]['id'], requests[1]['id']);
    expect(requests[0]['secret'], requests[1]['secret']);
    expect(requests[1]['secret'], hasLength(43));
    expect(requests[1]['id'], matches(RegExp(r'^[a-z0-9]{15}$')));
    expect(requests[1].containsKey('userId'), false);
    devices.dispose();
  });

  test('late device ID cannot bind the previous account', () async {
    final ready = Completer<String?>();
    var calls = 0;
    final pb = PocketBase(
      'https://pb.example',
      httpClientFactory: () => MockClient((_) async {
        calls++;
        return http.Response('{}', 200);
      }),
    );
    pb.authStore.save('first-token', RecordModel({'id': 'user00000000001'}));
    final devices = PushDevices(
      pocketBase: pb,
      storage: MemorySessionStorage(),
      deviceId: () => ready.future,
    );
    final first = devices.sync(enabled: true, language: 'ru');
    await Future<void>.delayed(Duration.zero);
    pb.authStore.save('second-token', RecordModel({'id': 'user00000000002'}));
    ready.complete('42');
    await first;
    expect(calls, 0);
    await devices.sync(enabled: false, language: 'en');
    expect(calls, 1);
    devices.dispose();
  });

  test('disable and click use persisted credentials; ordinary pushes have no tracking request', () async {
    final calls = <http.Request>[];
    final pb = PocketBase(
      'https://pb.example',
      httpClientFactory: () => MockClient((request) async {
        calls.add(request);
        return http.Response(
          request.url.path == '/api/push/devices' ? '{}' : '',
          request.url.path == '/api/push/devices' ? 200 : 204,
        );
      }),
    );
    pb.authStore.save('token', RecordModel({'id': 'user00000000001'}));
    final devices = PushDevices(
      pocketBase: pb,
      storage: MemorySessionStorage(),
      deviceId: () async => '42',
    );
    await devices.sync(enabled: true, language: 'ru');
    await devices.opened({'type': 'route', 'url': '/orders'});
    expect(calls, hasLength(1));
    await devices.opened({
      'pushRunId': 'run000000000001',
      'pushToken': 'click-token',
    });
    await devices.disable();
    expect(calls.map((r) => r.url.path), [
      '/api/push/devices',
      '/api/push/open',
      '/api/push/devices/disable',
    ]);
    final registration = jsonDecode(calls[0].body) as Map;
    final click = jsonDecode(calls[1].body) as Map;
    expect(click['deviceId'], registration['id']);
    expect(click['secret'], registration['secret']);
    devices.dispose();
    await devices.sync(enabled: true, language: 'ru');
    expect(calls, hasLength(3));
  });
}
