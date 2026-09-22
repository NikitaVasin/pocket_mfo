import 'dart:convert';
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:pocketbase/pocketbase.dart';

Map<String, dynamic> response({String mode = 'appView'}) => {
  'clickId': 'click-1',
  'expiresAt': '2026-09-22T12:00:00Z',
  'link': {
    'url': 'https://links.example/api/partnerlinks/r/encrypted-token',
    'mode': mode,
    'saveCooke': true,
    'changeClient': false,
    'showLoader': true,
    'openUrlsInBrowser': false,
    'skipWarningDialog': false,
    'title': 'Offer',
    'trackName': 'partner',
  },
};

final class Actions implements DynamicLinkActions {
  final browser = <Uri>[];
  final external = <Uri>[];
  @override
  Future<bool> openExternal(Uri uri) async {
    external.add(uri);
    return true;
  }

  @override
  Future<bool> openInAppBrowser(Uri uri) async {
    browser.add(uri);
    return true;
  }
}

void main() {
  test('parses the complete DynamicLink contract', () {
    final json = response();
    (json['link'] as Map<String, dynamic>)['warningDialog'] = {
      'title': 'Continue?',
      'content': 'Partner site',
    };
    final result = PartnerLinkResult.fromJson(json);
    expect(result.clickId, 'click-1');
    expect(result.expiresAt, DateTime.utc(2026, 9, 22, 12));
    expect(result.link.mode, DynamicLinkMode.appView);
    expect(result.link.saveCooke, isTrue);
    expect(result.link.changeClient, isFalse);
    expect(result.link.showLoader, isTrue);
    expect(result.link.warningDialog?.content, 'Partner site');
    expect(result.link.title, 'Offer');
    expect(result.link.trackName, 'partner');
  });

  test('rejects invalid URLs, flags, modes and expiry', () {
    for (final update in <void Function(Map<String, dynamic>)>[
      (j) => j['link']['url'] = 'javascript:alert(1)',
      (j) => j['link']['mode'] = 'unknown',
      (j) => j['link']['saveCooke'] = 'true',
      (j) => j['expiresAt'] = 'not-a-date',
    ]) {
      final json = response();
      update(json);
      expect(() => PartnerLinkResult.fromJson(json), throwsFormatException);
    }
  });

  test(
    'uses existing auth, requests fresh links and never follows the URL',
    () async {
      final requests = <http.Request>[];
      final pb = PocketBase(
        'https://api.example',
        httpClientFactory: () => MockClient((request) async {
          requests.add(request);
          return http.Response(jsonEncode(response()), 200);
        }),
      );
      pb.authStore.save(
        'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
        null,
      );
      for (var i = 0; i < 2; i++) {
        await pb.resolvePartnerLink(linkId: 'offer1');
      }
      expect(requests, hasLength(2));
      for (final request in requests) {
        expect(request.method, 'POST');
        expect(request.url.path, '/api/partnerlinks/links/offer1/resolve');
        expect(
          request.headers['Authorization'],
          'eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.test',
        );
        expect(request.body, isEmpty);
      }
    },
  );

  test('propagates API errors without opening or retrying', () async {
    var calls = 0;
    final pb = PocketBase(
      'https://api.example',
      httpClientFactory: () => MockClient((_) async {
        calls++;
        return http.Response('{"message":"Forbidden"}', 403);
      }),
    );
    await expectLater(
      pb.resolvePartnerLink(linkId: 'offer'),
      throwsA(isA<ClientException>()),
    );
    expect(calls, 1);
  });

  for (final mode in ['view', 'browser']) {
    testWidgets('opens the public redirect through $mode', (tester) async {
      final actions = Actions();
      final pb = PocketBase(
        'https://api.example',
        httpClientFactory: () => MockClient(
          (_) async => http.Response(jsonEncode(response(mode: mode)), 200),
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => TextButton(
              onPressed: () => pb.openPartnerLink(
                context,
                linkId: 'offer',
                actions: actions,
              ),
              child: const Text('Open'),
            ),
          ),
        ),
      );
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();
      final opened = mode == 'view' ? actions.browser : actions.external;
      expect(opened.single.path, '/api/partnerlinks/r/encrypted-token');
    });
  }

  testWidgets('passes the public URL directly to the mobile embedded view', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    addTearDown(() => debugDefaultTargetPlatformOverride = null);
    var requests = 0;
    final pb = PocketBase(
      'https://api.example',
      httpClientFactory: () => MockClient((_) async {
        requests++;
        return http.Response(jsonEncode(response()), 200);
      }),
    );
    DynamicLink? opened;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => pb.openPartnerLink(
              context,
              linkId: 'offer',
              embeddedViewBuilder: (_, link) {
                opened = link;
                return const Scaffold(body: Text('Partner WebView'));
              },
            ),
            child: const Text('Open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
    expect(find.text('Partner WebView'), findsOneWidget);
    expect(opened?.url.path, '/api/partnerlinks/r/encrypted-token');
    expect(requests, 1);
    debugDefaultTargetPlatformOverride = null;
  });

  testWidgets('does not navigate after the caller is disposed', (tester) async {
    final completion = Completer<http.Response>();
    final actions = Actions();
    final pb = PocketBase(
      'https://api.example',
      httpClientFactory: () => MockClient((_) => completion.future),
    );
    Future<bool>? opening;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () {
              opening = pb.openPartnerLink(
                context,
                linkId: 'offer',
                actions: actions,
              );
            },
            child: const Text('Open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Open'));
    await tester.pumpWidget(const SizedBox.shrink());
    completion.complete(
      http.Response(jsonEncode(response(mode: 'browser')), 200),
    );
    await tester.pumpAndSettle();
    expect(await opening, isFalse);
    expect(actions.external, isEmpty);
  });
}
