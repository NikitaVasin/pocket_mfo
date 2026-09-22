package dev.appbase.messaging;

import android.content.Context;
import android.content.Intent;
import androidx.annotation.NonNull;
import io.appmetrica.analytics.push.AppMetricaPush;
import io.flutter.embedding.engine.plugins.FlutterPlugin;
import io.flutter.embedding.engine.plugins.activity.ActivityAware;
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding;
import io.flutter.plugin.common.MethodChannel;
import io.flutter.plugin.common.PluginRegistry;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;

public final class AppMessagingPlugin implements FlutterPlugin, ActivityAware, PluginRegistry.NewIntentListener {
    private static final String CONSUMED = "dev.appbase.messaging.consumed";
    private final ArrayList<Map<String, String>> pending = new ArrayList<>();
    private MethodChannel channel;
    private Context context;
    private ActivityPluginBinding activityBinding;
    private boolean listening;

    @Override
    public void onAttachedToEngine(@NonNull FlutterPluginBinding binding) {
        context = binding.getApplicationContext();
        channel = new MethodChannel(binding.getBinaryMessenger(), "dev.appbase/app_messaging");
        channel.setMethodCallHandler((call, result) -> {
            if (call.method.equals("activate")) {
                AppMetricaPush.activate(context);
                // Activation fetches the existing Firebase token. The service
                // above forwards all later refreshes, even without a Dart engine.
                listening = true;
                ArrayList<Map<String, String>> launch = new ArrayList<>(pending);
                pending.clear();
                result.success(launch);
            } else if (call.method.equals("detach")) {
                listening = false;
                result.success(null);
            } else {
                result.notImplemented();
            }
        });
    }

    private void capture(Intent intent) {
        if (intent == null || intent.getBooleanExtra(CONSUMED, false)) return;
        String payload = intent.getStringExtra(AppMetricaPush.EXTRA_PAYLOAD);
        if (payload == null) return;
        intent.putExtra(CONSUMED, true);
        Map<String, String> action = new HashMap<>();
        action.put("id", UUID.randomUUID().toString());
        action.put("payload", payload);
        if (listening) channel.invokeMethod("action", action);
        else pending.add(action);
    }

    @Override
    public boolean onNewIntent(@NonNull Intent intent) {
        capture(intent);
        return false;
    }
    @Override
    public void onAttachedToActivity(@NonNull ActivityPluginBinding binding) {
        activityBinding = binding;
        binding.addOnNewIntentListener(this);
        capture(binding.getActivity().getIntent());
    }
    @Override
    public void onDetachedFromActivity() {
        if (activityBinding != null) activityBinding.removeOnNewIntentListener(this);
        activityBinding = null;
    }
    @Override
    public void onDetachedFromActivityForConfigChanges() { onDetachedFromActivity(); }
    @Override
    public void onReattachedToActivityForConfigChanges(@NonNull ActivityPluginBinding binding) { onAttachedToActivity(binding); }
    @Override
    public void onDetachedFromEngine(@NonNull FlutterPluginBinding binding) {
        listening = false;
        channel.setMethodCallHandler(null);
        onDetachedFromActivity();
    }
}
