import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:partner_links_flutter/partner_links_flutter.dart';
import 'package:push_links_flutter/push_links_flutter.dart';

PushAction push(Map<String, dynamic> data) =>
    PushAction(id: 'test', source: PushSource.appMetrica, data: data);
PartnerLinkResult result({bool warning = false}) => PartnerLinkResult(
  clickId: 'issued-once',
  expiresAt: DateTime.utc(2030),
  link: DynamicLink(
    url: Uri.parse('https://example.com/r/issued-once'),
    mode: DynamicLinkMode.browser,
    saveCooke: true,
    changeClient: true,
    showLoader: true,
    openUrlsInBrowser: false,
    skipWarningDialog: false,
    warningDialog: warning
        ? const DynamicLinkWarningDialog(title: 'Warning', content: 'Confirm')
        : null,
  ),
);

void main() {
  for (final brightness in Brightness.values) {
    testWidgets(
      'partner overlay preserves nested page and state ($brightness)',
      (tester) async {
        final root = GlobalKey<NavigatorState>();
        final nested = GlobalKey<NavigatorState>();
        final text = TextEditingController(text: 'keep my form');
        addTearDown(text.dispose);
        final response = Completer<PartnerLinkResult>();
        var requests = 0;
        String? requestedId;
        DynamicLink? opened;
        final resolver = PushLinkResolver(
          navigatorKey: root,
          onNavigate: (_) async {},
          resolvePartnerLink: (id) {
            requestedId = id;
            requests++;
            return response.future;
          },
          embeddedViewBuilder: (_, link) {
            opened = link;
            return Scaffold(
              appBar: AppBar(title: const Text('Partner')),
              body: const Text('WebView'),
            );
          },
        );
        await tester.pumpWidget(
          MaterialApp(
            navigatorKey: root,
            theme: ThemeData(brightness: brightness),
            home: Navigator(
              key: nested,
              onGenerateRoute: (_) => MaterialPageRoute<void>(
                builder: (_) => Scaffold(body: TextField(controller: text)),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final nestedState = nested.currentState;
        final opening = resolver(push({'type': 'partner', 'id': 'partner'}));
        await tester.pump();
        expect(find.byType(CircularProgressIndicator), findsNothing);
        expect(find.text('keep my form'), findsOneWidget);
        expect(requests, 1);
        expect(requestedId, 'partner');
        response.complete(result());
        await opening;
        await tester.pumpAndSettle();
        expect(opened!.mode, DynamicLinkMode.appView);
        expect(opened!.saveCooke, isTrue);
        expect(opened!.changeClient, isTrue);
        expect(opened!.url.toString(), 'https://example.com/r/issued-once');
        expect(nested.currentState, same(nestedState));
        root.currentState!.pop();
        await tester.pumpAndSettle();
        expect(find.text('keep my form'), findsOneWidget);
        expect(requests, 1);
      },
    );
  }

  testWidgets(
    'auth readiness prevents requests; failure is silent and preserves app',
    (tester) async {
      final key = GlobalKey<NavigatorState>();
      final auth = Completer<void>();
      var requests = 0;
      final resolver = PushLinkResolver(
        navigatorKey: key,
        onNavigate: (_) async {},
        beforeOpen: (_) => auth.future,
        resolvePartnerLink: (_) async {
          requests++;
          throw StateError('Denied');
        },
      );
      await tester.pumpWidget(
        MaterialApp(
          navigatorKey: key,
          home: const Scaffold(body: Text('App')),
        ),
      );
      final opening = resolver(push({'type': 'partner', 'id': 'partner'}));
      await tester.pump();
      expect(requests, 0);
      auth.complete();
      await opening;
      await tester.pumpAndSettle();
      expect(requests, 1);
      expect(find.textContaining('Не удалось открыть'), findsNothing);
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(key.currentState!.canPop(), isFalse);
      expect(find.text('App'), findsOneWidget);
    },
  );

  testWidgets('disposed resolver does not open a late response', (
    tester,
  ) async {
    final key = GlobalKey<NavigatorState>();
    final response = Completer<PartnerLinkResult>();
    var views = 0;
    final resolver = PushLinkResolver(
      navigatorKey: key,
      onNavigate: (_) async {},
      resolvePartnerLink: (_) => response.future,
      embeddedViewBuilder: (_, _) {
        views++;
        return const Text('WebView');
      },
    );
    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: key,
        home: const Scaffold(body: Text('App')),
      ),
    );
    final opening = resolver(push({'type': 'partner', 'id': 'partner'}));
    await tester.pump();
    resolver.dispose();
    response.complete(result());
    await opening;
    await tester.pumpAndSettle();
    expect(views, 0);
    expect(find.text('App'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('warning rejection never builds WebView', (tester) async {
    final key = GlobalKey<NavigatorState>();
    var views = 0;
    final resolver = PushLinkResolver(
      navigatorKey: key,
      onNavigate: (_) async {},
      resolvePartnerLink: (_) async => result(warning: true),
      embeddedViewBuilder: (_, _) {
        views++;
        return const Text('WebView');
      },
    );
    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: key,
        home: const Scaffold(body: Text('App')),
      ),
    );
    final opening = resolver(push({'type': 'partner', 'id': 'partner'}));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    await opening;
    await tester.pumpAndSettle();
    expect(views, 0);
    expect(find.text('App'), findsOneWidget);
  });

  testWidgets('missing partner result leaves the main screen unchanged', (
    tester,
  ) async {
    final key = GlobalKey<NavigatorState>();
    final resolver = PushLinkResolver(
      navigatorKey: key,
      onNavigate: (_) async {},
      resolvePartnerLink: (_) async => null,
    );
    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: key,
        home: const Scaffold(body: Text('App')),
      ),
    );
    await resolver(push({'type': 'partner', 'id': 'missing'}));
    await tester.pumpAndSettle();
    expect(find.text('App'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(key.currentState!.canPop(), isFalse);
  });

  test('in-app route delegates to application; malformed actions do not resolve a partner', () async {
    final routes = <Uri>[];
    var requests = 0;
    final resolver = PushLinkResolver(
      navigatorKey: GlobalKey(),
      onNavigate: (uri) async {
        routes.add(uri);
      },
      resolvePartnerLink: (_) async {
        requests++;
        return result();
      },
    );
    await resolver(push({'type': 'route', 'url': '/orders?id=42'}));
    expect(routes.single.toString(), '/orders?id=42');
    for (final data in [
      {'type': 'route', 'url': 'https://example.com'},
      {'type': 'route', 'url': '//example.com'},
      {'type': 'partner', 'id': ''},
      {'type': 'unknown'},
    ]) {
      await expectLater(resolver(push(data)), throwsFormatException);
    }
    expect(requests, 0);
  });
}
