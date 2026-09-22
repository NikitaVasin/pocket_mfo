# dynamic_link

Чистый Dart-пакет с описанием динамической ссылки. Он не зависит от Flutter и
подходит для domain, data, backend и других платформонезависимых слоёв.

```dart
import 'package:dynamic_link/dynamic_link.dart';

final link = DynamicLink(
  url: Uri.parse('https://example.com'),
  mode: .appView,
  saveCooke: true,
  changeClient: false,
  showLoader: true,
  openUrlsInBrowser: false,
  skipWarningDialog: false,
);
```

Пакет только хранит намерение открытия. Исполнение режимов находится в
`dynamic_link_flutter`.
