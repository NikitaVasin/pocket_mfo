package com.mfohub.dynamiclinkflutter;
import io.flutter.embedding.engine.plugins.FlutterPlugin;
import io.flutter.plugin.common.MethodChannel;
import android.webkit.CookieManager;
import android.webkit.WebStorage;
import android.webkit.WebView;

public final class DynamicLinkFlutterPlugin implements FlutterPlugin {
    private MethodChannel channel;
    @Override public void onAttachedToEngine(FlutterPluginBinding binding) {
        channel = new MethodChannel(binding.getBinaryMessenger(), "dynamic_link_flutter/web_data");
        channel.setMethodCallHandler((call, result) -> {
            if (!call.method.equals("clear")) { result.notImplemented(); return; }
            try {
                WebStorage.getInstance().deleteAllData();
                WebView view = new WebView(binding.getApplicationContext());
                view.clearCache(true);
                view.clearFormData();
                view.destroy();
                CookieManager.getInstance().removeAllCookies(removed -> {
                    CookieManager.getInstance().flush();
                    result.success(null);
                });
            } catch (Exception error) { result.error("clear_failed", error.getClass().getSimpleName(), null); }
        });
    }
    @Override public void onDetachedFromEngine(FlutterPluginBinding binding) { channel.setMethodCallHandler(null); }
}
