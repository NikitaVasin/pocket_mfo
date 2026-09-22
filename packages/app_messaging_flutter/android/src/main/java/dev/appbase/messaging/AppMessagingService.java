package dev.appbase.messaging;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import androidx.annotation.NonNull;
import com.google.firebase.messaging.RemoteMessage;
import io.appmetrica.analytics.push.provider.firebase.AppMetricaMessagingService;
import io.flutter.plugins.firebase.messaging.FlutterFirebaseMessagingService;

/** A single FCM service. FlutterFire's receiver handles ordinary FCM messages. */
public final class AppMessagingService extends FlutterFirebaseMessagingService {
    @Override
    public void onNewToken(@NonNull String token) {
        super.onNewToken(token);
        new AppMetricaMessagingService().processToken(this, token);
    }

    @Override
    public void onMessageReceived(@NonNull RemoteMessage message) {
        if (AppMetricaMessagingService.isNotificationRelatedToSDK(message)) {
            new AppMetricaMessagingService().processPush(this, message);
        } else {
            super.onMessageReceived(message);
            showForegroundNotification(message);
        }
    }

    private void showForegroundNotification(RemoteMessage message) {
        // FCM displays notification messages itself in the background. This
        // callback receives their foreground deliveries; data-only stays silent.
        RemoteMessage.Notification content = message.getNotification();
        if (content == null) return;
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return;
        Intent intent = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (intent == null) return;
        intent.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        for (java.util.Map.Entry<String, String> item : message.getData().entrySet()) intent.putExtra(item.getKey(), item.getValue());
        intent.putExtra("google.message_id", message.getMessageId());
        int id = message.getMessageId() != null ? message.getMessageId().hashCode() : (int) System.currentTimeMillis();
        PendingIntent tap = PendingIntent.getActivity(this, id, intent, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        NotificationManager manager = getSystemService(NotificationManager.class);
        Notification.Builder builder;
        if (Build.VERSION.SDK_INT >= 26) {
            manager.createNotificationChannel(new NotificationChannel("app_messaging", "Notifications", NotificationManager.IMPORTANCE_HIGH));
            builder = new Notification.Builder(this, "app_messaging");
        } else builder = new Notification.Builder(this);
        manager.notify(id, builder.setSmallIcon(R.drawable.app_messaging_notification)
            .setContentTitle(content.getTitle()).setContentText(content.getBody())
            .setAutoCancel(true).setContentIntent(tap).build());
    }
}
