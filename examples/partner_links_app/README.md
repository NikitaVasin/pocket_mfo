# Flutter-пример

Запускаемое приложение Android, iOS и web. Вход через `demo_members`, чтение офферов из `partner_links`, новый resolve на каждое открытие, нативный DynamicLink-адаптер, список своих заказов из `conversations`. Работа с сервером отделена в DemoRepository, состояние — DemoModel.

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

Предзаполнен публичный демопользователь `default@variants.test` / `demo-variants-123`. Другие пользователи из Go example подходят для проверки Variants. Сессия хранится только в памяти приложения.

Без настройки AppMetrica доступны вход, чтение офферов и заказов; открытие покажет 503 с пояснением. Для полного потока:

1. Запустите сервер с подключённым Partner Links по [серверной инструкции](../../plugins/partnerlinks/README.md). Ключи шифрования не нужны.
2. В верхней панели админки откройте «Партнёрские ссылки», задайте Base URL, Application ID и Post API key. Выберите поле профиля `id`: приложение передаёт ID авторизованного пользователя в AppMetrica SDK.
3. В `partner_links` задайте реальный URL и провайдера. Общие режим и предупреждение меняются в `dynamic_link_settings` отдельно для каждого набора Variants.
4. Пришедшие постбеки обновят `conversations`; нажмите «Обновить» в примере. Запись создаётся при выдаче ссылки; пример показывает только записи после первого постбека (`status != "pending"`). ID заявки обязателен для Revenue.

Android/iOS подключают `app_messaging_flutter` и `push_links_flutter`. Google-файлы относятся к `dev.appbase.example`; Android applicationId и iOS bundle ID совпадают. В примере задан тестовый AppMetrica API key `fbca87ec-97c5-4df4-b4aa-aed80630f2fa`. Это клиентские конфигурации, не серверные ключи отправителя. При замене Google-файлов выполните из директории приложения `fvm dart run app_messaging_flutter:configure`.

После первого кадра SDK инициализируется с `requestPermissionOnStart: true`. Кнопка «Разрешить уведомления» запрашивает разрешение вручную. При входе AppMetrica получает ID пользователя, при выходе он сбрасывается; resolve не передаёт `profileId` или сведения об устройстве. Для реальных отчётов профиль должен предварительно попасть в AppMetrica из SDK.

Payload `{"type":"route","url":"/orders"}` открывает список заказов; `/offers` возвращает к офферам. Payload `{"type":"partner","id":"demopartner0001"}` запрашивает ссылку и при успехе добавляет WebView поверх всего приложения. Пока запрос выполняется, при ошибке или отсутствии ссылки экран не меняется. После холодного запуска действия, которым нужна авторизация, ждут входа. Кнопки «Проверить переход» и «Проверить экран поверх» запускают тот же callback без отправки пуша.

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

После web-сборки из корня `npm run test:flutter` проверяет Chromium с временным PocketBase: вход, офферы, заказы, ошибку resolve без настройки AppMetrica и выход. Пользовательская база не затрагивается.
