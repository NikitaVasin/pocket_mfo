# Flutter-пример

Запускаемое приложение Android, iOS и web. Автоматический гостевой вход и обычная авторизация через `users`, чтение офферов из `partner_links`, новый resolve на каждое открытие, нативный DynamicLink-адаптер, список своих заказов из `conversations`. Работа с сервером отделена в DemoRepository, состояние — DemoModel.

## Запуск

Из корня репозитория:

```sh
fvm install
fvm flutter pub get
GOTOOLCHAIN=auto go run ./example serve --http=127.0.0.1:8090
```

Go example использует демонстрационные миграции. В другом терминале:

```sh
cd examples/partner_links_app
fvm flutter run -d chrome
```

Для Android-эмулятора:

```sh
fvm flutter run --dart-define=POCKETBASE_URL=http://10.0.2.2:8090
```

Для iOS Simulator используйте `http://localhost:8090`. Для физического устройства укажите доступный ему HTTPS-адрес сервера и выберите собственную signing team в Xcode. В Android debug разрешён локальный HTTP, в iOS — local networking; release-настройки безопасности приложения определяйте отдельно. Публичный Base URL плагина также должен быть доступен устройству.

Предзаполнен публичный демопользователь `default@variants.test` / `demo-variants-123`. Другие пользователи из Go example подходят для проверки Variants. На Android/iOS сессия и пароль гостя сохраняются в защищённом хранилище; web-пример хранит их только в памяти. При выходе создаётся новый гость.

Без настройки AppMetrica доступны вход, чтение офферов и заказов; открытие покажет 503 с пояснением. Для полного потока:

1. Запустите сервер с подключённым Partner Links по [серверной инструкции](../../plugins/partnerlinks/README.md). Ключи шифрования не нужны.
2. В верхней панели админки откройте «Партнёрские ссылки», задайте Base URL, Application ID и Post API key. Profile ID всегда равен `user.id`; выбирать поле не требуется.
3. В `partner_links` задайте реальный URL и провайдера. Общие режим и предупреждение меняются в `dynamic_link_settings` отдельно для каждого набора Variants.
4. Пришедшие постбеки обновят `conversations`; нажмите «Обновить» в примере. Запись создаётся при выдаче ссылки; пример показывает только записи после первого постбека (`status != "pending"`). ID заявки обязателен для Revenue.

Интеграцией владеет [pocket_mfo_flutter](../../packages/pocket_mfo_flutter/README.md). Для настоящей аналитики Android/iOS передайте `--dart-define=APPMETRICA_SDK_KEY=YOUR_CLIENT_KEY`. Без ключа и в web работает PreviewAnalytics, который не отправляет события. Post API key остаётся на сервере. Кнопка «Отправить тестовое событие» вызывает `PocketMfo.reportEvent`; текущий пользователь и карта экспериментов отображаются на экране.

Пуши включаются на Android/iOS при наличии SDK key. Google-файлы относятся к `dev.appbase.example`; при замене выполните `fvm dart run app_messaging_flutter:configure`. Разрешение запрашивается кнопкой «Разрешить уведомления», автоматического запроса при старте нет.

Payload `{"type":"route","url":"/orders"}` открывает заказы; `/offers` возвращает к офферам. Payload `{"type":"partner","id":"demopartner0001"}` получает ссылку и открывает её через существующий PushLinkResolver. Холодный запуск ждёт гостевой авторизации и корневого Navigator. Ошибка разрешения партнёрской ссылки сохраняет текущий экран.

Конфигурация отправителей FCM/APNs и Apple signing выполняется отдельно. Проверка реальной доставки и нажатий требует устройства/поддерживаемого симулятора и настроенных кабинетов. Web продолжает работать без инициализации мобильных SDK.

Web использует браузерный адаптер вместо WebView. При блокировке новой вкладки разрешите всплывающие окна. WebView, cookies и файловый выбор проверяйте на Android/iOS.

## Проверки

Из корня:

```sh
fvm flutter analyze
fvm flutter test examples/partner_links_app/test
cd examples/partner_links_app
fvm flutter build web --no-web-resources-cdn
```

После web-сборки из корня `npm run test:flutter` проверяет Chromium с временным PocketBase: автоматический гостевой старт, обычный вход, событие, офферы, заказы, ошибку resolve без настройки AppMetrica и выход. Пользовательская база не затрагивается.
