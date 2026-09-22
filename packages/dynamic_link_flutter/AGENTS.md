# Пакет dynamic_link_flutter

- Используй пакет только во Flutter-коде, когда нужно открыть `DynamicLink`,
  показать warning dialog или встроенный WebView.
- Импортируй публичный API через
  `package:dynamic_link_flutter/dynamic_link_flutter.dart`.
- Пакет зависит от `dynamic_link`, но не должен импортировать Ozaim, его тему,
  локализацию, Provider, Bloc или router.
- Для application-specific DI передавай `DynamicLinkActions` в
  `link.open(context, actions: ...)`; пакет не должен выбирать DI-фреймворк.
- `appView` использует WebView на Android/iOS и системный in-app browser на
  остальных платформах. Android file picker держи за conditional import.
- Изменения выбора режима и навигации покрывай widget-тестами.
