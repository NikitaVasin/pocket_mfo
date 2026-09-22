# App Messaging Flutter

- Firebase Core + Messaging, AppMetrica Analytics и нативный AppMetrica Push. Не добавляйте Firebase Analytics/Crashlytics без запроса.
- `onAction` обязателен. Пакет не зависит от PocketBase, роутера или бизнес-логики приложения.
- Разрешение запрашивается вручную или через `requestPermissionOnStart`. Само получение, silent push и закрытие уведомления не вызывают действие.
- Сохраняйте обработку холодного запуска, обновления токенов без Dart engine и единственный пользовательский Android FCM service. На iOS сохраняйте цепочку делегатов FlutterFire, передавайте в AppMetrica APNs token.
- Не логируйте payload, токены и идентификаторы профиля. Google-конфигурации клиента не заменяют серверные ключи отправителя.
- Проверяйте новые случаи жизненного цикла тестами. Изменения нативного кода проверяйте сборками Android и iOS.
