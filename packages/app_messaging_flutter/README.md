# App Messaging Flutter

Один пакет для Firebase Core + Messaging, AppMetrica Analytics и нативного AppMetrica Push SDK. Android и iOS. PocketBase, навигация и бизнес-логика в пакет не входят.

## Подключение

1. Добавьте пакет в зависимости приложения.
2. Положите `google-services.json` в `android/app/`, `GoogleService-Info.plist` в `ios/Runner/`. Идентификаторы приложения должны совпадать с файлами.
3. Из директории приложения выполните:

```sh
fvm flutter pub get
fvm dart run app_messaging_flutter:configure
```

Команда подключает Google Services Gradle plugin, добавляет plist в ресурсы Runner, включает Push Notifications и background remote notifications, объединяет существующие entitlements. Повторный запуск безопасен. Ручные изменения AppDelegate/MainActivity и `firebase_options.dart` не нужны. Для iOS требуется macOS и Ruby gem `xcodeproj >= 1.27` (входит в актуальный CocoaPods), включая проекты на Swift Package Manager.

Автоконфигуратор рассчитан на стандартные Flutter-проекты: модуль Android `app`, буквальный `applicationId`, iOS target `Runner`, без Android flavors. Для нестандартного проекта подключите `com.google.gms.google-services` к app вручную; в Xcode добавьте Google plist в Copy Bundle Resources, включите Push Notifications/background remote notifications и добавьте Info.plist `AppMessagingAPNSEnvironment` со значением, совпадающим с `aps-environment` вашей подписи. Не отключайте Firebase method swizzling.

В стандартном проекте команда задаёт `APP_MESSAGING_APNS_ENVIRONMENT`: Debug — `development`, Profile/Release — `production`. Это значение используется и entitlements, и SDK. При собственной схеме подписи измените build setting соответственно. Apple signing и настройку отправителя в кабинетах Firebase/AppMetrica выполняет владелец приложения.

## Инициализация

Создайте один экземпляр на всё приложение. Инициализируйте после первого кадра с готовым Navigator либо дождитесь готовности приложения внутри callback:

```dart
final messaging = AppMessaging();

await messaging.initialize(
  appMetricaApiKey: 'YOUR_APPMETRICA_KEY',
  onAction: (action) async {
    await applicationReady;
    await resolveAction(action.data);
  },
  requestPermissionOnStart: true, // по умолчанию false
  onError: (error, stack) {
    // Ошибка SDK или обработчика. Не логируйте содержимое уведомления.
  },
);

final permission = await messaging.requestPermission();
final current = await messaging.getPermission();
final token = await messaging.getFcmToken();
await messaging.setUserProfileId(user.id);
// При выходе:
await messaging.setUserProfileId(null);
```

Инициализация выполняется один раз на экземпляр. Отказ в уведомлениях возвращается как статус. Повторно показать системный диалог после окончательного отказа невозможно: разрешение меняется в настройках ОС. FCM token на iOS может быть `null`, пока не завершилась регистрация APNs; это не блокирует запуск приложения.

`onAction` обязателен. Получение уведомления и silent push не вызывают навигацию. Нажатия при холодном запуске сохраняются до инициализации; последующие действия обрабатываются последовательно. Повтор одного идентификатора подавляется в пределах текущего экземпляра (последние 128). Callback должен завершаться после выполнения действия, не ждать закрытия открытой страницы. Очередь не является постоянным хранилищем и не гарантирует exactly-once между перезапусками процесса.

На Android один `FirebaseMessagingService` передаёт AppMetrica её сообщения и обновления FCM token, обычные сообщения обслуживает FlutterFire. Текущий токен SDK получает при активации. На iOS нативный мост регистрирует APNs token в AppMetrica, а Firebase самостоятельно связывает его с FCM. Вручную передавать FCM token в AppMetrica из Dart не нужно.

Обычные FCM notification-сообщения показываются и в foreground; data-only остаются тихими. В iOS обработчик сохраняет цепочку делегатов FlutterFire и различает получение, нажатие и закрытие уведомления. Изображения через iOS Notification Service Extension и расширенная статистика доставки background-пушей требуют отдельного расширения и не входят в этот пакет.

## Данные действия

AppMetrica: укажите JSON-объект в «Дополнительных данных» (`data` в Push API). FCM: передайте поля в `data` либо JSON-строку в `data.payload`. Базовый пакет не задаёт структуру объекта:

```json
{"customAction":"show_order","orderId":"42"}
```

Готовый callback для маршрутов и партнёрских ссылок находится в [push_links_flutter](../push_links_flutter/README.md).

Основа интеграции: [Firebase Flutter](https://firebase.google.com/docs/cloud-messaging/flutter/get-started), [совместная обработка Android](https://appmetrica.yandex.com/docs/ru/sdk/android/push/android-other-push-services-settings), [AppMetrica iOS](https://appmetrica.yandex.com/docs/en/sdk/ios/push/quick-start).

## Проверки

```sh
fvm flutter test packages/app_messaging_flutter/test
```

Unit-тесты проверяют очередь, разрешения, дедупликацию, ошибки и конфигурацию Android. Отправку через FCM/APNs необходимо отдельно проверить на устройстве с настроенными кабинетами.

`messaging.permissionChanges` — поток результатов `requestPermission()`, включая запрос при старте. Изменения через настройки ОС проверяйте вызовом `getPermission()` при возвращении приложения на экран. Поток закрывается при `dispose()`.
