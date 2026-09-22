import 'package:dynamic_link_flutter/dynamic_link_flutter.dart';
import 'package:dynamic_link_flutter/src/dynamic_link_web_data.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/in_memory_shared_preferences_async.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_async_platform_interface.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('dynamic_link_flutter/web_data');
  setUp(() {
    SharedPreferencesAsyncPlatform.instance =
        InMemorySharedPreferencesAsync.empty();
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
  });
  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    DynamicLinkWebData.closeViews.clear();
  });
  test('очистка закрывает страницы, удаляет только WebView preferences и вызывает native cleanup', () async {
    final preferences = SharedPreferencesAsync();
    await preferences.setStringList('webview.cookies.example.com', [
      'session=private',
    ]);
    await preferences.setString('other.setting', 'keep');
    var closed = false;
    var nativeCalls = 0;
    DynamicLinkWebData.closeViews.add(() async {
      closed = true;
    });
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          expect(closed, isTrue);
          expect(call.method, 'clear');
          nativeCalls++;
          return null;
        });
    await clearDynamicLinkWebData();
    expect(
      await preferences.getStringList('webview.cookies.example.com'),
      isNull,
    );
    expect(await preferences.getString('other.setting'), 'keep');
    expect(nativeCalls, 1);
    await clearDynamicLinkWebData();
    expect(nativeCalls, 2);
  });
}
