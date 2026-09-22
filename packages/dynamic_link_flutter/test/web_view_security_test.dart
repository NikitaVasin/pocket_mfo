import 'dart:async';

import 'package:dynamic_link_flutter/dynamic_link_flutter.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/in_memory_shared_preferences_async.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_async_platform_interface.dart';
import 'package:webview_flutter_platform_interface/webview_flutter_platform_interface.dart';

void main() {
  late FakeWebView platform;
  const channel = MethodChannel('dynamic_link_flutter/web_data');
  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    SharedPreferencesAsyncPlatform.instance =
        InMemorySharedPreferencesAsync.empty();
    platform = FakeWebView();
    WebViewPlatform.instance = platform;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (_) async => null);
  });
  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });

  Future<void> mount(WidgetTester tester, Brightness brightness) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData(brightness: brightness),
        home: Scaffold(
          body: DynamicLinkWebView(
            uri: Uri.parse('https://example.com/private'),
            saveCooke: true,
            changeClient: false,
            protectFromHistoryTrap: true,
          ),
        ),
      ),
    );
    await tester.pump();
  }

  for (final brightness in Brightness.values) {
    testWidgets('native cookies are not reconstructed or copied: $brightness', (
      tester,
    ) async {
      final preferences = SharedPreferencesAsync();
      await preferences.setStringList('webview.cookies.example.com', [
        'session=old',
      ]);
      await preferences.setString('app.setting', 'keep');
      await mount(tester, brightness);
      platform.delegate.finished?.call('https://example.com/private');
      await tester.pumpAndSettle();
      expect(platform.cookieWrites, isEmpty);
      expect(platform.controller.scripts, isNot(contains('document.cookie')));
      expect(
        await preferences.getStringList('webview.cookies.example.com'),
        isNull,
      );
      expect(await preferences.getString('app.setting'), 'keep');
      expect(platform.controller.loads, [
        Uri.parse('https://example.com/private'),
      ]);
      debugDefaultTargetPlatformOverride = null;
    });

    for (final clear in [false, true]) {
      testWidgets(
        'pending WebView initialization stops after ${clear ? 'clear' : 'dispose'}: $brightness',
        (tester) async {
          platform.controller.channelReady = Completer<void>();
          await mount(tester, brightness);
          expect(platform.controller.loads, isEmpty);
          if (clear) {
            await clearDynamicLinkWebData();
          } else {
            await tester.pumpWidget(const SizedBox.shrink());
          }
          platform.controller.channelReady!.complete();
          await tester.pumpAndSettle();
          expect(
            platform.controller.loads.where((uri) => uri.scheme == 'https'),
            isEmpty,
          );
          expect(tester.takeException(), isNull);
          debugDefaultTargetPlatformOverride = null;
        },
      );
    }
  }
}

class FakeWebView extends WebViewPlatform {
  final controller = FakeController();
  late FakeDelegate delegate;
  final cookieWrites = <WebViewCookie>[];

  @override
  PlatformWebViewController createPlatformWebViewController(
    PlatformWebViewControllerCreationParams params,
  ) => controller;
  @override
  PlatformNavigationDelegate createPlatformNavigationDelegate(
    PlatformNavigationDelegateCreationParams params,
  ) => delegate = FakeDelegate(params);
  @override
  PlatformWebViewWidget createPlatformWebViewWidget(
    PlatformWebViewWidgetCreationParams params,
  ) => FakeWidget(params);
  @override
  PlatformWebViewCookieManager createPlatformCookieManager(
    PlatformWebViewCookieManagerCreationParams params,
  ) => FakeCookies(params, cookieWrites);
}

class FakeController extends PlatformWebViewController {
  FakeController()
    : super.implementation(const PlatformWebViewControllerCreationParams());
  final loads = <Uri>[];
  final scripts = <String>[];
  Completer<void>? channelReady;
  @override
  Future<void> setJavaScriptMode(JavaScriptMode mode) async {}
  @override
  Future<void> setPlatformNavigationDelegate(
    PlatformNavigationDelegate handler,
  ) async {}
  @override
  Future<void> loadRequest(LoadRequestParams params) async {
    loads.add(params.uri);
  }

  @override
  Future<void> addJavaScriptChannel(JavaScriptChannelParams params) async {
    await channelReady?.future;
  }

  @override
  Future<void> runJavaScript(String script) async {
    scripts.add(script);
  }

  @override
  Future<Object> runJavaScriptReturningResult(String script) async {
    scripts.add(script);
    return script == 'document.cookie' ? 'session=secret' : false;
  }

  @override
  Future<bool> canGoBack() async => false;
}

class FakeDelegate extends PlatformNavigationDelegate {
  FakeDelegate(super.params) : super.implementation();
  PageEventCallback? finished;
  @override
  Future<void> setOnPageStarted(PageEventCallback callback) async {}
  @override
  Future<void> setOnPageFinished(PageEventCallback callback) async {
    finished = callback;
  }

  @override
  Future<void> setOnProgress(ProgressCallback callback) async {}
  @override
  Future<void> setOnNavigationRequest(
    NavigationRequestCallback callback,
  ) async {}
}

class FakeWidget extends PlatformWebViewWidget {
  FakeWidget(super.params) : super.implementation();
  @override
  Widget build(BuildContext context) => const SizedBox.expand();
}

class FakeCookies extends PlatformWebViewCookieManager {
  FakeCookies(super.params, this.writes) : super.implementation();
  final List<WebViewCookie> writes;
  @override
  Future<void> setCookie(WebViewCookie cookie) async {
    writes.add(cookie);
  }
}
