# dynamic_link_flutter

Flutter-адаптер для `dynamic_link`. Пакет показывает warning dialog, выбирает
способ открытия и предоставляет готовые `DynamicLinkWebView` и
`DynamicLinkWebViewScreen`.

```dart
import 'package:dynamic_link_flutter/dynamic_link_flutter.dart';

final opened = await link.open(context);
```

Для тестов или собственного платформенного слоя передай реализацию
`DynamicLinkActions` в `actions`. Для режима `appView` можно передать
`embeddedViewBuilder` и заменить стандартный экран.

Заголовки кнопок warning dialog берутся из `MaterialLocalizations`. Встроенный
WebView используется на Android и iOS; на остальных платформах `appView`
безопасно переключается на системный in-app browser.
