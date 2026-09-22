import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:partner_links_example/main.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:pocketbase/pocketbase.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('example restores a session and displays users and offers', (
    tester,
  ) async {
    final pb = PocketBase(
      'http://example.test',
      httpClientFactory: () => MockClient((request) async {
        if (request.url.path.endsWith('/auth-refresh')) {
          return http.Response(
            jsonEncode({
              'token': 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
              'record': {
                'id': 'user',
                'collectionName': 'users',
                'collectionId': 'users',
              },
            }),
            200,
          );
        }
        if (request.url.path == '/api/variants/me') {
          return http.Response('{"experiments":{"offers":"default"}}', 200);
        }
        return http.Response(
          '{"items":[],"page":1,"perPage":500,"totalPages":1,"totalItems":0}',
          200,
        );
      }),
    );
    pb.authStore.save(
      'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
      RecordModel.fromJson({
        'id': 'user',
        'collectionName': 'users',
        'collectionId': 'users',
      }),
    );
    final app = PocketMfo(
      pocketBase: pb,
      authCollection: 'users',
      appMetricaConfig: const AppMetricaConfig('preview'),
      analytics: PreviewAnalytics(),
      storage: MemorySessionStorage(),
    );
    await tester.pumpWidget(PartnerLinksExample(integration: app));
    await tester.pumpAndSettle();
    expect(find.text('Офферы'), findsOneWidget);
    expect(find.textContaining('Пользователь: user'), findsOneWidget);
    await tester.tap(find.text('Отправить тестовое событие'));
    await tester.pumpAndSettle();
    expect(find.text('Событие отправлено'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
