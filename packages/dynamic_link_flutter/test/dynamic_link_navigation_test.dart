import 'dart:async';

import 'package:dynamic_link_flutter/dynamic_link_flutter.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late _FakeDynamicLinkActions actions;

  setUp(() => actions = _FakeDynamicLinkActions());

  tearDown(() => debugDefaultTargetPlatformOverride = null);

  Future<void> pumpLink(
    WidgetTester tester,
    DynamicLink link, {
    DynamicLinkEmbeddedViewBuilder? embeddedViewBuilder,
  }) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: FilledButton(
              key: const Key('open-link'),
              onPressed: () => unawaited(
                link.open(
                  context,
                  actions: actions,
                  embeddedViewBuilder: embeddedViewBuilder,
                ),
              ),
              child: const Text('Open'),
            ),
          ),
        ),
      ),
    );
  }

  testWidgets('warning cancellation prevents navigation', (tester) async {
    await pumpLink(tester, _link(mode: .browser, warning: true));

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(actions.external, isEmpty);
    expect(actions.inApp, isEmpty);
  });

  testWidgets('warning confirmation continues requested mode', (tester) async {
    await pumpLink(tester, _link(mode: .view, warning: true));

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Continue'));
    await tester.pumpAndSettle();

    expect(actions.inApp, [Uri.parse('https://example.com')]);
  });

  testWidgets('skipWarningDialog bypasses the warning', (tester) async {
    await pumpLink(
      tester,
      _link(mode: .browser, warning: true, skipWarning: true),
    );

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();

    expect(find.text('Warning'), findsNothing);
    expect(actions.external, [Uri.parse('https://example.com')]);
  });

  testWidgets('browser mode uses the external application', (tester) async {
    await pumpLink(tester, _link(mode: .browser));

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();

    expect(actions.external, [Uri.parse('https://example.com')]);
  });

  testWidgets('non-http schemes always use the external application', (
    tester,
  ) async {
    await pumpLink(
      tester,
      _link(mode: .appView, url: Uri.parse('mailto:test@example.com')),
    );

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();

    expect(actions.external, [Uri.parse('mailto:test@example.com')]);
    expect(actions.inApp, isEmpty);
  });

  testWidgets('appView falls back to the in-app browser on desktop', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = .linux;
    await pumpLink(tester, _link(mode: .appView));

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();

    expect(actions.inApp, [Uri.parse('https://example.com')]);
    debugDefaultTargetPlatformOverride = null;
  });

  testWidgets('appView opens the supplied embedded view on mobile', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = .android;
    await pumpLink(
      tester,
      _link(mode: .appView),
      embeddedViewBuilder: (context, link) =>
          const Scaffold(body: Text('Embedded view')),
    );

    await tester.tap(find.byKey(const Key('open-link')));
    await tester.pumpAndSettle();

    expect(find.text('Embedded view'), findsOneWidget);
    expect(actions.inApp, isEmpty);
    expect(actions.external, isEmpty);
    debugDefaultTargetPlatformOverride = null;
  });
}

DynamicLink _link({
  required DynamicLinkMode mode,
  bool warning = false,
  bool skipWarning = false,
  Uri? url,
}) {
  return DynamicLink(
    url: url ?? Uri.parse('https://example.com'),
    mode: mode,
    saveCooke: false,
    changeClient: false,
    showLoader: true,
    openUrlsInBrowser: false,
    skipWarningDialog: skipWarning,
    warningDialog: warning
        ? const DynamicLinkWarningDialog(title: 'Warning', content: 'Continue?')
        : null,
  );
}

final class _FakeDynamicLinkActions() implements DynamicLinkActions {
  final List<Uri> inApp = [];
  final List<Uri> external = [];

  @override
  Future<bool> openExternal(Uri uri) async {
    external.add(uri);
    return true;
  }

  @override
  Future<bool> openInAppBrowser(Uri uri) async {
    inApp.add(uri);
    return true;
  }
}
