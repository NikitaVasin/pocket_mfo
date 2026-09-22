# Push Links Flutter

Готовый callback для [AppMessaging](../app_messaging_flutter/README.md): обычная навигация внутри приложения и открытие партнёрской ссылки поверх всего приложения.

```dart
final rootNavigatorKey = GlobalKey<NavigatorState>();
final resolver = PushLinkResolver.pocketBase(
  navigatorKey: rootNavigatorKey, // тот же ключ у MaterialApp/корневого router
  pocketBase: pb,                // текущий клиент с authStore приложения
  beforeOpen: (action) async {
    await applicationReady;
    if (action is PartnerPushAction) await authenticatedSessionReady;
  },
  onNavigate: (uri) async {
    router.go(uri.toString()); // навигацией управляет приложение
  },
);

await messaging.initialize(
  appMetricaApiKey: 'YOUR_APPMETRICA_KEY',
  onAction: resolver.call,
  requestPermissionOnStart: true,
);
```

## Формат payload

Путь внутри приложения, включая query/fragment:

```json
{"type":"route","url":"/orders?id=42"}
```

ID записи коллекции `partner_links`:

```json
{"type":"partner","id":"demopartner0001"}
```

AppMetrica: JSON в «Дополнительных данных». FCM: тот же объект в `data` или JSON-строка в `data.payload`. Для FCM notification-сообщения добавьте `notification.title`/`notification.body`; data-only самостоятельно интерфейс не открывают.

Маршрут должен начинаться с `/`; внешние URL и `//host` не принимаются. Приложение проверяет разрешённые маршруты в `onNavigate`.

## Партнёрское открытие

После `beforeOpen` адаптер делает один `resolvePartnerLink` через переданный PocketBase. Во время запроса пользователь остаётся на текущем экране: индикатора и промежуточной страницы нет. Ошибка, невалидный ответ сервера или `null` из пользовательского загрузчика не показывают сообщение и не меняют навигацию. Автоматических повторов нет.

При успехе адаптер вызывает **push на корневом Navigator**. Текущий экран, его форма и стек вложенного Navigator сохраняются; после закрытия WebView пользователь возвращается к ним. Режим принудительно `appView`, независимо от обычного режима оффера. Политики cookies, user agent, загрузчика WebView, внешних переходов и обязательного предупреждения сохраняются. Warning dialog показывается только после успешного resolve. Публичный redirect заранее не запрашивается, второй клик не отправляется.

В конструктор `PushLinkResolver` можно передать свой `resolvePartnerLink`, возвращающий `Future<PartnerLinkResult?>`, и `embeddedViewBuilder`. При уничтожении владельца вызовите `resolver.dispose()`: поздний ответ запроса не откроет экран. Завершите также `messaging.dispose()`.

## Проверки

```sh
fvm flutter test packages/push_links_flutter/test
```

Тестируются обе темы, сохранение вложенной навигации и формы, ожидание авторизации, тихий отказ, пустой результат, отмена предупреждения и поздний ответ после dispose.
