import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:partner_links_example/main.dart';
import 'package:pocket_mfo_flutter/pocket_mfo_flutter.dart';
import 'package:pocketbase/pocketbase.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('offline retries are handled and preserve guest credentials', (
    tester,
  ) async {
    var offline = true;
    final identities = <String>[];
    final passwords = <String>[];
    var creates = 0;
    final pb = PocketBase(
      'http://example.test',
      httpClientFactory: () => MockClient((request) async {
        if (request.url.path.endsWith('/auth-with-password')) {
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          identities.add(body['identity'] as String);
          passwords.add(body['password'] as String);
          if (offline) throw http.ClientException('Connection refused');
          return http.Response(
            jsonEncode({
              'token': 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
              'record': {
                'id': identities.last,
                'collectionName': 'users',
                'collectionId': 'users',
              },
            }),
            200,
          );
        }
        if (request.url.path == '/api/collections/users/records') creates++;
        return http.Response(
          jsonEncode({
            'experiments': <String, String>{},
            'items': [],
            'page': 1,
            'perPage': 500,
            'totalPages': 1,
            'totalItems': 0,
          }),
          200,
        );
      }),
    );
    final sdk = PocketMfo(
      pocketBase: pb,
      authCollection: 'users',
      appMetricaConfig: const AppMetricaConfig('preview'),
      analytics: PreviewAnalytics(),
      storage: MemorySessionStorage(),
    );
    addTearDown(sdk.dispose);
    await tester.pumpWidget(PartnerLinksExample(integration: sdk));
    await tester.pumpAndSettle();
    expect(find.textContaining('недоступен'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('Повторить'));
    // Let the failed request complete before FutureBuilder rebuilds.
    await tester.idle();
    await tester.pumpAndSettle();
    expect(find.text('Повторить'), findsOneWidget);
    expect(tester.takeException(), isNull);
    offline = false;
    await tester.tap(find.text('Повторить'));
    await tester.pumpAndSettle();
    expect(find.text('Офферы'), findsOneWidget);
    expect(identities, hasLength(3));
    expect(identities.toSet(), hasLength(1));
    expect(passwords.toSet(), hasLength(1));
    expect(creates, 0);
    expect(tester.takeException(), isNull);
    await tester.runAsync(sdk.dispose);
    await tester.pumpWidget(const SizedBox());
  });

  for (final restored in [false, true]) {
    testWidgets(
      'SDK initializes ${restored ? "restored" : "new guest"} session without an email form',
      (tester) async {
        const token = 'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test';
        String? id = restored ? 'user' : null;
        String? password;
        var creates = 0;
        final requests = <String>[];
        Map<String, Object?> record() => {
          'id': id,
          'collectionName': 'users',
          'collectionId': 'users',
        };
        http.Response json(Object body, [int status = 200]) =>
            http.Response(jsonEncode(body), status);
        final pb = PocketBase(
          'http://example.test',
          httpClientFactory: () => MockClient((request) async {
            requests.add(request.url.path);
            if (request.url.path.endsWith('/auth-refresh')) {
              return json({'token': token, 'record': record()});
            }
            if (request.url.path.endsWith('/auth-with-password')) {
              final body = jsonDecode(request.body) as Map<String, dynamic>;
              if (id == null ||
                  body['identity'] != id ||
                  body['password'] != password) {
                return json({'message': 'Invalid credentials'}, 400);
              }
              return json({'token': token, 'record': record()});
            }
            if (request.url.path == '/api/collections/users/records') {
              final body = jsonDecode(request.body) as Map<String, dynamic>;
              id = body['id'] as String;
              password = body['password'] as String;
              expectSync(id!.length, 15);
              expectSync(password!.length, 48);
              expectSync(body['email'], '');
              creates++;
              return json(record());
            }
            expectSync(request.headers['Authorization'], token);
            if (request.url.path == '/api/variants/me') {
              return json({
                'experiments': {'offers': 'default'},
              });
            }
            return json({
              'items': [],
              'page': 1,
              'perPage': 500,
              'totalPages': 1,
              'totalItems': 0,
            });
          }),
        );
        if (restored) pb.authStore.save(token, RecordModel.fromJson(record()));
        final sdk = PocketMfo(
          pocketBase: pb,
          authCollection: 'users',
          appMetricaConfig: const AppMetricaConfig('preview'),
          analytics: PreviewAnalytics(),
          storage: MemorySessionStorage(),
        );
        addTearDown(sdk.dispose);
        await tester.pumpWidget(PartnerLinksExample(integration: sdk));
        await tester.pumpAndSettle();
        expect(find.text('Офферы'), findsOneWidget);
        expect(find.textContaining('Пользователь: $id'), findsOneWidget);
        expect(find.byType(TextField), findsNothing);
        expect(find.text('Войти'), findsNothing);
        expect(creates, restored ? 0 : 1);
        final authCount = requests
            .where((path) => path.contains('/users/'))
            .length;
        await tester.tap(find.byTooltip('Обновить'));
        await tester.pumpAndSettle();
        expect(
          requests.where((path) => path.contains('/users/')).length,
          authCount,
        );
        await tester.tap(find.text('Отправить тестовое событие'));
        await tester.pumpAndSettle();
        expect(find.text('Событие отправлено'), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.runAsync(sdk.dispose);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
}
