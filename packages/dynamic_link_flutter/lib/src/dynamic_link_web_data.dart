import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Очистка cookies, cache, local storage, IndexedDB и других native данных
/// встроенного WebView. Внешние браузеры принадлежат ОС и не очищаются.
Future<void> clearDynamicLinkWebData() => DynamicLinkWebData.clear();

/// Координирует очистку с уже открытыми WebView и отложенной инициализацией.
abstract final class DynamicLinkWebData {
  static int generation = 0;
  static final closeViews = <Future<void> Function()>{};
  // Remove only legacy JavaScript snapshots, never native cookies or app data.
  static Future<void> removeLegacyCookies() async {
    final preferences = SharedPreferencesAsync();
    final keys = await preferences.getKeys();
    for (final key in keys.where((key) => key.startsWith('webview.cookies.'))) {
      await preferences.remove(key);
    }
  }

  static Future<void> clear() async {
    generation++;
    await Future.wait(List.of(closeViews).map((close) => close()));
    await removeLegacyCookies();
    if (!kIsWeb &&
        [
          TargetPlatform.android,
          TargetPlatform.iOS,
        ].contains(defaultTargetPlatform)) {
      await const MethodChannel('dynamic_link_flutter/web_data')
          .invokeMethod<void>('clear');
    }
  }
}
