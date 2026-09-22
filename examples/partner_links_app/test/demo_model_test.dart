import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pocketbase/pocketbase.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:partner_links_example/main.dart';
import 'package:partner_links_example/data/demo_repository.dart';
import 'package:partner_links_example/ui/demo_model.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('repository uses authentication and server ownership rules', () async {
    final paths = <String>[];
    final pb = PocketBase(
      'http://localhost',
      httpClientFactory: () => MockClient((request) async {
        paths.add(request.url.path);
        if (request.url.path == '/api/variants/me') {
          return http.Response('{"experiments":{}}', 200);
        }
        if (request.url.path.contains('/conversations/')) {
          expect(request.url.queryParameters['filter'], 'status != "pending"');
        }
        if (request.url.path.endsWith('auth-with-password')) {
          return http.Response(
            jsonEncode({
              'token': 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
              'record': {
                'id': 'user',
                'collectionId': 'members',
                'collectionName': 'users',
              },
            }),
            200,
          );
        }
        expect(
          request.headers['Authorization'],
          'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
        );
        return http.Response(
          jsonEncode({
            'page': 1,
            'totalPages': 1,
            'totalItems': 1,
            'items': [
              {'id': 'record', 'name': 'Offer', 'status': 'approved'},
            ],
          }),
          200,
        );
      }),
    );
    final integration = PocketMfo(
      pocketBase: pb,
      authCollection: 'users',
      appMetricaConfig: const AppMetricaConfig('test'),
      storage: MemorySessionStorage(),
      analytics: PreviewAnalytics(),
    );
    addTearDown(integration.dispose);
    final model = DemoModel(DemoRepository(integration));
    await model.login('user@example.test', 'password');
    expect(model.signedIn, isTrue);
    expect(model.offers.single.id, 'record');
    expect(model.orders.single.getStringValue('status'), 'approved');
    expect(paths, [
      '/api/collections/users/auth-with-password',
      '/api/variants/me',
      '/api/collections/partner_links/records',
      '/api/collections/conversations/records',
    ]);
    model.dispose();
  });

  test('resolve errors are visible, with no automatic retry', () async {
    var calls = 0;
    final pb = PocketBase(
      'http://localhost',
      httpClientFactory: () => MockClient((request) async {
        if (request.url.path.endsWith('/auth-refresh')) {
          return http.Response(
            jsonEncode({
              'token': 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
              'record': {
                'id': 'user',
                'collectionName': 'users',
                'collectionId': 'members',
              },
            }),
            200,
          );
        }
        if (request.url.path == '/api/variants/me') {
          return http.Response('{"experiments":{}}', 200);
        }
        calls++;
        expect(request.body, isEmpty);
        return http.Response('{"message":"Not configured"}', 503);
      }),
    );
    pb.authStore.save(
      'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
      RecordModel.fromJson({
        'id': 'user',
        'collectionName': 'users',
        'collectionId': 'members',
      }),
    );
    final integration = PocketMfo(
      pocketBase: pb,
      authCollection: 'users',
      appMetricaConfig: const AppMetricaConfig('test'),
      storage: MemorySessionStorage(),
      analytics: PreviewAnalytics(),
    );
    addTearDown(integration.dispose);
    final model = DemoModel(DemoRepository(integration));
    expect(await model.resolve('offer'), isNull);
    expect(model.error, contains('Сервер ещё не настроен'));
    expect(model.busy, isFalse);
    expect(calls, 1);
    model.dispose();
  });
}
