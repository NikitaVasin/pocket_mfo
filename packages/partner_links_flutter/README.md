# partner_links_flutter

Получение партнёрских ссылок из Go-плагина PocketBase и открытие через `dynamic_link_flutter`. Требуется Dart ≥3.13.2; пакеты проверены на Flutter 3.47.5 / Dart 3.13.4.

```yaml
dependencies:
  partner_links_flutter:
    path: ../pocket_mfo/packages/partner_links_flutter
```

Методы предоставляет extension `PocketBasePartnerLinks` на существующем экземпляре `PocketBase`, отдельный клиент создавать не нужно.

Соседние `dynamic_link` и `dynamic_link_flutter` подключаются автоматически по path. Пакеты входят в корневой workspace этого репозитория и не требуют workspace из `mfo_hub`. Используйте `fvm flutter pub get` из корня.

```dart
import 'package:partner_links_flutter/partner_links_flutter.dart';

final opened = await authenticatedPocketBase.openPartnerLink(
  context,
  linkId: offer.partnerLinkId,
);
```

PocketBase должен быть авторизован обычным пользователем разрешённой сервером auth-коллекции. AppMetrica profileId равен PocketBase user.id; задайте этот ID SDK или используйте [pocket_mfo_flutter](../pocket_mfo_flutter/README.md). Передавать profileId в запросе не нужно. Пакет не подключает SDK, не вычисляет эксперименты и не отправляет второй клик. User ID и назначения определяет сервер.

Отдельное получение DTO:

```dart
final result = await authenticatedPocketBase.resolvePartnerLink(linkId: linkId);
// result.clickId, result.expiresAt, result.link — DynamicLink
await result.link.open(context);
```

Не выполняйте предварительный GET/HEAD публичного URL и не кешируйте результат для следующих нажатий. Каждый вызов `openPartnerLink`/`resolvePartnerLink` выдаёт новый токен, а клик отправляется при GET-редиректе в WebView/браузере. Никаких автоматических повторов HTTP в клиенте нет.

Режимы: `appView` — WebView Android/iOS и встроенный браузер ОС на остальных платформах; `view` — встроенный браузер ОС; `browser` — внешнее приложение. Все параметры `DynamicLink`, включая `saveCooke`, warning dialog, title и category, передаются без переименования. Для своего платформенного слоя передайте `actions`, `embeddedViewBuilder` или `warningDialogBuilder`.

`ClientException` PocketBase передаётся вызывающему коду. Неполный/неверный DTO вызывает `FormatException`; пустой linkId — `ArgumentError`. При удалённом во время запроса Flutter-контексте `openPartnerLink` возвращает false. Возвращаемый bool открытия наследует семантику `dynamic_link_flutter` и не подтверждает загрузку страницы или запись конверсии.

Полный пример кнопки с блокировкой повторного нажатия и обработкой API-ошибки: [example/example.dart](example/example.dart).

AppMetrica принимает profileId, известный SDK; назначенному в первой и единственной сессии профилю может потребоваться повторный вход. Подробности: [серверная документация](../../plugins/partnerlinks/README.md).

Проверки из соответствующих каталогов:

```sh
# packages/dynamic_link
fvm dart analyze
fvm dart test

# packages/dynamic_link_flutter и packages/partner_links_flutter
fvm flutter analyze
fvm flutter test
```

Widget-тесты не заменяют проверку реального партнёрского сайта на Android/iOS: file picker, внешние приложения и сохранение cookies зависят от ОС и сайта.

Запускаемое приложение Android/iOS/web: [partner_links_app](../../examples/partner_links_app/README.md).

`VariantExposure.fromRecord(record)` читает подписанный variantContext. Передайте список в `resolvePartnerLink(exposures: shown)` / `openPartnerLink(exposures: shown)` для привязки к показанному материалу. Сервер проверяет tokens; нижний пакет сам показ не отправляет. [Воронки](../../docs/EXPERIMENT_FUNNELS.md).
