# Разработка

Поддерживаем PocketBase **v0.40.4** и Go **1.27**. Для браузерных проверок используем Node.js 24 и Playwright из `package-lock.json`; для smoke-теста нужен Docker с Compose v2.

## Локальный цикл

1. Прочитайте [AGENTS.md](AGENTS.md) и инструкции затрагиваемого плагина.
2. Воспроизведите ошибку тестом на внешне наблюдаемое поведение. Исправляйте причину, сохраняя публичный контракт остальных плагинов.
3. Отформатируйте изменённые Go-файлы через `gofmt` и запустите тесты пакета.
4. Перед передачей изменения выполните общие проверки:

```sh
GOTOOLCHAIN=auto go test -race ./...
GOTOOLCHAIN=auto go vet ./...
GOTOOLCHAIN=auto go build ./...
GOTOOLCHAIN=auto go mod verify
GOTOOLCHAIN="$(go env GOVERSION)" go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
npm ci
npx playwright install chromium
npm run test:browser
npm run test:docker
npm run test:partner
npm run test:repository
fvm install
fvm flutter pub get --enforce-lockfile
fvm flutter analyze --no-pub
fvm dart test packages/dynamic_link/test
fvm flutter test --no-pub packages/pocket_mfo_flutter/test packages/dynamic_link_flutter/test packages/partner_links_flutter/test packages/app_messaging_flutter/test packages/push_links_flutter/test examples/partner_links_app/test
fvm dart analyze plugins/typedconfig/testdata
fvm dart run plugins/typedconfig/testdata/smoke.dart
npm run test:integration
(cd examples/partner_links_app && fvm flutter build web --no-pub --no-web-resources-cdn)
npm run test:flutter
git diff --check
```

CI повторяет Go, браузерные, Docker- и Dart/Flutter-проверки, компиляцию примеров руководства в независимых проектах, Typed Config DTO и web smoke. Версия SDK берётся из `.fvmrc`; [flutter-action](https://github.com/subosito/flutter-action#use-version-from-pubspecyaml-or-fvm-config) загружает её для установки FVM, проверки выполняются через FVM. Анализатор govulncheck должен запускаться toolchain проекта: `go run ...@version` с `GOTOOLCHAIN=auto` сам по себе может выбрать минимальный SDK инструмента вместо Go из go.mod. На Linux для Playwright могут потребоваться системные зависимости: `npx playwright install --with-deps chromium`.

## Стенды

- `example` всегда включает Schema Lock; использует тестовые миграции с открытыми демонстрационными паролями. Запускайте его только как демостенд.
- `tests/testapp` подключает плагины без Schema Lock, чтобы сохранять покрытие редакторов схемы.
- Playwright запускает обе конфигурации с временными БД на портах 8097 и 8099, без повторного использования чужого сервера. Один worker нужен из-за общих демонстрационных данных в рамках стенда.
- Docker smoke использует собственный проект и volume, порт 8098. Он не удаляет volume обычного example. Свой порт можно задать через `POCKETBASE_TEST_PORT`.

## Что включать в изменение

Объясните проблему, новое поведение и фактически выполненные проверки. Изменение публичного API сопровождайте документацией. Для безопасности проверяйте прямой API, batch и сохранность данных после отказа. Для интерфейса — обе темы, CRUD, relation picker и взаимодействие плагинов.

Не редактируйте сгенерированные служебные данные вручную и не включайте базы, архивы резервных копий, credentials, node_modules или результаты тестов в коммит. Не обновляйте PocketBase без проверки всех экспериментальных UI extensions и списка маршрутов Schema Lock.

## Границы автоматических проверок

Нативные изменения дополнительно проверяйте в `examples/partner_links_app`: `fvm flutter build apk --debug --no-pub` и на macOS `fvm flutter build ios --simulator --no-codesign --no-pub`. Сборка не подтверждает доставку FCM/APNs, события/Revenue или воронку в живой AppMetrica. Для этого нужны согласованные кабинеты, устройства и сценарии из [руководства интеграции](docs/AI_INTEGRATION.md).

`test:repository` проверяет локальные Markdown-ссылки, индексы инструкций всех плагинов, module path и отсутствие баз/приватных ключей/артефактов среди файлов для Git. Это не универсальный сканер секретов: просмотрите итоговый diff отдельно. Клиентские Google-конфигурации example содержат публичные идентификаторы демопроекта; серверные service-account/APNs ключи в Git недопустимы.
