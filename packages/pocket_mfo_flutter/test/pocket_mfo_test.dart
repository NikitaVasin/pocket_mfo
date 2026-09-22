import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pocketbase/pocketbase.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';

const token = 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.signature';
const expired = 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjF9.signature';
Map<String, dynamic> record(String id) => {
  'id': id,
  'collectionName': 'users',
  'collectionId': 'userscollection',
};

class Analytics implements PocketMfoAnalytics {
  String? id;
  int activations = 0;
  final events = <Map<String, Object?>>[];
  Completer<void>? sending;
  @override
  Future<void> activate(AppMetricaConfig config, String userId) async {
    activations++;
    id = userId;
  }

  @override
  Future<void> setUserId(String? userId) async {
    id = userId;
  }

  @override
  Future<void> reportEvent(String name, Map<String, Object?> parameters) async {
    await sending?.future;
    events.add({'id': id, 'name': name, ...parameters});
  }
}

class Backend {
  final users = <String, String>{'member': 'correct'};
  final requests = <http.Request>[];
  int creates = 0;
  int refreshCode = 200;
  int experimentsCode = 200;
  int? authCode;
  bool loseCreateResponse = false;
  Completer<http.Response>? delayedExperiments;
  Map<String, String> assignments = {'offers': 'premium/checkout/B'};
  http.Response json(Object body, [int code = 200]) =>
      http.Response(jsonEncode(body), code);
  Future<http.Response> handle(http.Request request) async {
    requests.add(request);
    final body = request.body.isEmpty
        ? <String, dynamic>{}
        : jsonDecode(request.body) as Map<String, dynamic>;
    if (request.url.path == '/api/variants/me') {
      if (delayedExperiments != null) return delayedExperiments!.future;
      return json({
        'items': <Object?>[],
        'experiments': assignments,
      }, experimentsCode);
    }
    if (request.url.path.endsWith('/auth-refresh')) {
      return json({
        'token': token,
        'record': record(users.keys.last),
      }, refreshCode);
    }
    if (request.url.path.endsWith('/auth-with-password')) {
      if (authCode != null) return json({'message': 'auth error'}, authCode!);
      final id = body['identity'] as String;
      if (users[id] != body['password']) {
        return json({'message': 'Invalid credentials'}, 400);
      }
      return json({'token': token, 'record': record(id)});
    }
    if (request.url.path.endsWith('/records') && request.method == 'POST') {
      final id = body['id'] as String;
      if (users.containsKey(id)) return json({'message': 'duplicate'}, 400);
      users[id] = body['password'] as String;
      creates++;
      if (loseCreateResponse) throw http.ClientException('offline');
      return json(record(id));
    }
    throw StateError('Unexpected request ${request.url}');
  }

  PocketBase client() => PocketBase(
    'https://example.test',
    httpClientFactory: () => MockClient(handle),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Backend backend;
  late Analytics analytics;
  late MemorySessionStorage storage;
  late PocketMfo app;
  late PocketBase pb;
  final diagnostics = <Object>[];
  PocketMfo create(PocketBase client) => PocketMfo(
    pocketBase: client,
    authCollection: 'users',
    appMetricaConfig: const AppMetricaConfig('fake-sdk-key'),
    storage: storage,
    analytics: analytics,
    onError: (error, _) => diagnostics.add(error),
  );
  setUp(() {
    backend = Backend();
    analytics = Analytics();
    storage = MemorySessionStorage();
    diagnostics.clear();
    pb = backend.client();
    app = create(pb);
  });
  tearDown(() => app.dispose());

  test(
    'concurrent initialize creates one guest, preserves client authStore',
    () async {
      final authStore = pb.authStore;
      await Future.wait([app.initialize(), app.initialize(), app.initialize()]);
      expect(backend.creates, 1);
      expect(app.isGuest, isTrue);
      expect(analytics.id, app.user!.id);
      expect(identical(pb.authStore, authStore), isTrue);
      expect(analytics.activations, 1);
      final registration = backend.requests.firstWhere(
        (r) => r.url.path.endsWith('/records'),
      );
      final data = jsonDecode(registration.body) as Map<String, dynamic>;
      expect((data['password'] as String).length, 48);
      expect(data['password'], data['passwordConfirm']);
      expect(data['email'], '');
    },
  );

  test(
    'restores same guest after expired token without another create',
    () async {
      await app.initialize();
      final id = app.user!.id;
      await app.dispose();
      backend.refreshCode = 401;
      pb = backend.client();
      app = create(pb);
      await app.initialize();
      expect(app.user!.id, id);
      expect(backend.creates, 1);
    },
  );

  test(
    'lost create response recovers the persisted credentials on retry',
    () async {
      backend.loseCreateResponse = true;
      await expectLater(app.initialize(), throwsA(isA<ClientException>()));
      expect(backend.creates, 1);
      await app.dispose();
      backend.loseCreateResponse = false;
      pb = backend.client();
      app = create(pb);
      await app.initialize();
      expect(backend.creates, 1);
      expect(app.isGuest, isTrue);
    },
  );

  test(
    'events use snapshot without HTTP and do not mutate caller data',
    () async {
      await app.initialize();
      final count = backend.requests.length;
      final params = <String, Object?>{
        'experiments': 'forged',
        'nested': {'x': 1},
        'null': null,
      };
      await app.reportEvent('tap', parameters: params);
      await app.reportEvent('tap');
      expect(backend.requests.length, count);
      expect(params['experiments'], 'forged');
      expect(analytics.events.first['experiments'], {
        'offers': 'premium/checkout/B',
      });
      expect(analytics.events.first['id'], app.user!.id);
    },
  );

  test(
    'failed snapshot omits reserved key, later success supports empty map',
    () async {
      backend.experimentsCode = 503;
      await app.initialize();
      await app.reportEvent(
        'tap',
        parameters: {
          'experiments': {'fake': 'x'},
        },
      );
      expect(analytics.events.single.containsKey('experiments'), isFalse);
      expect(diagnostics, isNotEmpty);
      backend.experimentsCode = 200;
      backend.assignments = {};
      await app.refreshExperiments();
      await app.reportEvent('tap');
      expect(analytics.events.last['experiments'], isEmpty);
    },
  );

  test(
    'snapshot refresh failure retains last successful session snapshot',
    () async {
      await app.initialize();
      backend.experimentsCode = 503;
      await app.refreshExperiments();
      await app.reportEvent('tap');
      expect(analytics.events.single['experiments'], backend.assignments);
    },
  );

  test('login changes SDK identity and logout creates a new guest', () async {
    await app.initialize();
    final guest = app.user!.id;
    await app.signIn('member', 'correct');
    expect(app.isGuest, isFalse);
    await app.reportEvent('member');
    expect(analytics.events.last['id'], 'member');
    await app.logout();
    expect(app.isGuest, isTrue);
    expect(app.user!.id, isNot(guest));
    expect(analytics.id, app.user!.id);
    expect(analytics.activations, 1);
  });

  test(
    'invalid existing ordinary session requires explicit login or logout',
    () async {
      pb.authStore.save(expired, RecordModel.fromJson(record('member')));
      backend.refreshCode = 401;
      await expectLater(
        app.initialize(),
        throwsA(isA<PocketMfoAuthRequired>()),
      );
      expect(backend.creates, 0);
      await app.signIn('member', 'correct');
      expect(analytics.id, 'member');
    },
  );

  test('network errors never replace a user with a guest', () async {
    pb.authStore.save(expired, RecordModel.fromJson(record('member')));
    backend.refreshCode = 503;
    await expectLater(app.initialize(), throwsA(isA<ClientException>()));
    expect(pb.authStore.record!.id, 'member');
    expect(backend.creates, 0);
  });

  test(
    'unexpired session can initialize offline and omit experiments',
    () async {
      pb.authStore.save(token, RecordModel.fromJson(record('member')));
      backend.refreshCode = 503;
      backend.experimentsCode = 503;
      await app.initialize();
      await app.reportEvent('offline');
      expect(analytics.events.single['id'], 'member');
      expect(analytics.events.single.containsKey('experiments'), isFalse);
      expect(backend.creates, 0);
    },
  );

  test(
    'late snapshot from previous account cannot contaminate new one',
    () async {
      await app.initialize();
      final delayed = Completer<http.Response>();
      backend.delayedExperiments = delayed;
      final refresh = app.refreshExperiments();
      await Future<void>.delayed(Duration.zero);
      backend.delayedExperiments = null;
      backend.assignments = {'offers': 'default'};
      await app.signIn('member', 'correct');
      delayed.complete(
        backend.json({
          'experiments': {'offers': 'wrong/A'},
        }),
      );
      await refresh;
      await app.reportEvent('tap');
      expect(analytics.events.last['id'], 'member');
      expect(analytics.events.last['experiments'], {'offers': 'default'});
    },
  );

  test('events and SDK profile switching are serialized', () async {
    await app.initialize();
    final guest = app.user!.id;
    analytics.sending = Completer<void>();
    final event = app.reportEvent('before-login');
    await Future<void>.delayed(Duration.zero);
    final login = app.signIn('member', 'correct');
    analytics.sending!.complete();
    await Future.wait([event, login]);
    expect(analytics.events.single['id'], guest);
    expect(analytics.id, 'member');
  });

  test(
    'parallel refresh coalesces and timeout cannot install late data',
    () async {
      await app.initialize();
      final delayed = Completer<http.Response>();
      backend.delayedExperiments = delayed;
      final first = app.refreshExperiments();
      final second = app.refreshExperiments();
      expect(identical(first, second), isTrue);
      await first;
      expect(diagnostics, isNotEmpty);
      delayed.complete(
        backend.json({
          'experiments': {'offers': 'late'},
        }),
      );
      await Future<void>.delayed(Duration.zero);
      expect(app.experiments, {'offers': 'premium/checkout/B'});
    },
  );
  test('dispose discards an outstanding experiment response', () async {
    await app.initialize();
    final delayed = Completer<http.Response>();
    backend.delayedExperiments = delayed;
    final refreshing = app.refreshExperiments();
    await app.dispose();
    delayed.complete(
      backend.json({
        'experiments': {'offers': 'late'},
      }),
    );
    await refreshing;
    expect(app.experiments, isNull);
    expect(() => app.refreshExperiments(), throwsStateError);
  });

  test(
    'failed ordinary login preserves the active guest and snapshot',
    () async {
      await app.initialize();
      final guest = app.user!.id;
      await expectLater(
        app.signIn('member', 'wrong'),
        throwsA(isA<ClientException>()),
      );
      await app.reportEvent('still-guest');
      expect(app.isGuest, isTrue);
      expect(analytics.events.single['id'], guest);
      expect(analytics.events.single['experiments'], backend.assignments);
    },
  );

  test('different server namespaces do not restore each other', () async {
    await app.initialize();
    final guest = app.user!.id;
    await app.dispose();
    final otherBackend = Backend();
    final other = PocketBase(
      'https://another.test',
      httpClientFactory: () => MockClient(otherBackend.handle),
    );
    app = create(other);
    await app.initialize();
    expect(app.user!.id, isNot(guest));
    expect(otherBackend.creates, 1);
  });
}
