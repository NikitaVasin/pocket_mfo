# Pocket MFO — плагины PocketBase

Набор подключаемых Go-плагинов и общий стенд для их проверки.

**Интеграция в приложение с AI-агентом:** [единое руководство](docs/AI_INTEGRATION.md) — обязательное интервью, устройство всех плагинов, подключение Go/Flutter, необходимые ключи и файлы, настройка AppMetrica/Firebase/APNs, постбеков и MCP, проверки и передача результата.

| Плагин | Возможности |
| --- | --- |
| [Polymorphic Relation](plugins/polymorphicrelation) | Одно поле с одним родителем из нескольких коллекций; штатные relation-столбцы, нативная админка, `expand` |
| [Variants](plugins/variants) | Варианты контента по аудитории, эксперименты по бакетам, нативные API rules/realtime, текущие назначения и история |
| [Typed Config](plugins/typedconfig) | Типизированные экранные блоки, формы и карточки, ссылки с mapping и каскадом, генерация Dart DTO |
| [Singleton](plugins/singleton) | Одна запись на коллекцию или набор Variants; включение в админке, форма вместо таблицы |
| [Dynamic Link](plugins/dynamiclink) | Переиспользуемое поле с URL и настройками WebView/браузера |
| [Partner Links](plugins/partnerlinks) | Партнёрские ссылки с хранением конверсий, настройки провайдеров, постбеки AppMetrica и Flutter-клиент |
| [Push](plugins/push) | Устройства, аудитории, черновики кампаний, ручной/отложенный запуск через AppMetrica и атрибуция конверсий |
| [MCP](plugins/mcp) | Отдельная вкладка ключей, права на инструменты и коллекции, наполнение контента и подключение других плагинов через `Provider` |
| [Schema Lock](plugins/schemalock) | Схема только из кода; записи, Variants и настройки сервера доступны, системные записи только для чтения, кроме `_superusers` |
| [Currency Rates](plugins/currencyrates) | Курсы ЦБ РФ в `currency_rates`, ежедневное обновление, история за последние пять лет |

Поддерживаемая версия: **PocketBase v0.40.4**, Go **1.27**. UI API этой версии экспериментальный: обновление PocketBase требует повторного запуска интеграционных и браузерных тестов. Форк PocketBase не нужен.

## Запуск example

Из корня репозитория:

```sh
docker compose -f example/compose.yaml up --build
```

Админка: <http://localhost:8090/_/>. В том же Compose запускается [демо партнёр](example/demopartner/README.md) на <http://localhost:8091/>: переходы на Google, смена статусов заявок, постбеки и отдельный volume. Миграция автоматически создаёт тестового суперпользователя **`admin@admin.com`** с паролем **`123456`**, в том числе при обновлении существующего example. Если такой пользователь уже есть, его пароль сохраняется. Эти учётные данные предназначены для локального стенда.

В example включён **Schema Lock**: доступен CRUD записей и суперпользователей, а также кнопка **Variants** на странице обычной коллекции. Настройки сервера, cron, логи, экспорт схемы и бэкапы доступны. Редактирование схемы, SQL, импорт, восстановление бэкапов и переключение Singleton закрыты через UI и HTTP API. Остальные системные записи доступны только для чтения. Схему и настройки Singleton задавайте в Go/миграциях; базовые правила Variants меняйте через `variants.Publish`. Описанные ниже редакторы схемы доступны при подключении плагинов без Schema Lock.

Миграция создаёт `articles`, `videos`, `comments` и по одному комментарию к статье и видео. Поле `comments.subject` необязательное, удаление родителя по умолчанию запрещено. Чтение демоданных открыто; изменение доступно суперпользователю. Для авторизации используется единая коллекция `users`.

Для плагина Variants создаются `users` (6 пользователей), `demo_subscriptions` (3 подписки) и `demo_offers` (12 записей в 6 наборах). Настройки открываются кнопкой **Variants** коллекции `demo_offers`; выбор набора — селекторами над записями. Общий пароль демопользователей: **`demo-variants-123`**.

| Email в `users` | Выбранный набор |
| --- | --- |
| `default@variants.test` | Default |
| `newcomer@variants.test` | Новички |
| `premium-a@variants.test` | Premium → эксперимент «Формат подборки» → A |
| `premium-b@variants.test` | Premium → эксперимент «Формат подборки» → B |
| `subscriber@variants.test` | Оплаченная подписка; правило выше Premium |
| `split@variants.test` | Default: активность и оплата относятся к разным подпискам |

Каждый пользователь получает две записи через обычный API `demo_offers`; гость получает Default, суперпользователь видит все наборы. A/B делит бакеты пополам. Базовый набор Premium доступен после отключения экспериментов. Для проверки участия авторизуйтесь через `users` и вызовите `GET /api/variants/me?collection=demo_offers`; история наблюдавшихся назначений появляется после обращений пользователя. В админке можно выбрать демопользователя в предпросмотре Variants без добавления записи в историю.

Данные хранятся в Docker volume `pb_data`. Обычные перезапуски и `docker compose down` сохраняют их; повторный запуск не дублирует seed и не перезаписывает изменения. Для другого порта задайте `POCKETBASE_PORT`, например:

```sh
POCKETBASE_PORT=8091 docker compose -f example/compose.yaml up --build
```

Локально, без Docker:

```sh
GOTOOLCHAIN=auto go run ./example serve --http=127.0.0.1:8090 --dir=./example/pb_data
```

И локальный запуск, и Docker собирают плагины из **этого checkout**. Импорты `github.com/NikitaVasin/pocket_mfo/plugins/...` совпадают с module path в корневом go.mod и разрешаются в локальные `plugins/...`; копия этих плагинов с GitHub не скачивается. При первой сборке Go может скачать внешние зависимости и нужную версию toolchain. Отдельный go.mod или replace для example не требуется.

В example также есть `demo_screen_configs → Главная витрина`: редактор типизированных блоков без JSON. [Документация](plugins/typedconfig/README.md).

## Подключение

Все плагины — пакеты одного Go-модуля. Подключайте только нужные пакеты до `Bootstrap` / `Start`:

```go
import (
    "log"

    "github.com/pocketbase/pocketbase"
    "github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
    "github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
    "github.com/NikitaVasin/pocket_mfo/plugins/singleton"
    "github.com/NikitaVasin/pocket_mfo/plugins/variants"
)

func main() {
    app := pocketbase.New()
    polymorphicrelation.Register(app)
    variants.Register(app)
    singleton.Register(app)
    schemalock.Register(app)
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

Go module path: `github.com/NikitaVasin/pocket_mfo`. После отправки исходников в [репозиторий](https://github.com/NikitaVasin/pocket_mfo) подключение из другого проекта:

```sh
go get github.com/NikitaVasin/pocket_mfo@latest
```

Для воспроизводимых сборок фиксируйте выбранную версию или commit. Для локальной разработки используйте `replace github.com/NikitaVasin/pocket_mfo => ../pocket_mfo`. Порядок публикации и вариант приватного репозитория описаны в [инструкции](docs/PUBLISHING.md).

UI встроен в Go-бинарник через `embed.FS`; отдельная сборка frontend не нужна. Конфигурации Variants и Singleton хранятся в служебных записях: для полного переноса используйте резервную копию, одного экспорта схемы недостаточно.

| Документация | Инструкции для агентов |
| --- | --- |
| [Polymorphic Relation](plugins/polymorphicrelation/README.md) | [AGENTS.md](plugins/polymorphicrelation/AGENTS.md) |
| [Variants](plugins/variants/README.md) | [AGENTS.md](plugins/variants/AGENTS.md) |
| [Typed Config](plugins/typedconfig/README.md) | [AGENTS.md](plugins/typedconfig/AGENTS.md) |
| [Singleton](plugins/singleton/README.md) | [AGENTS.md](plugins/singleton/AGENTS.md) |
| [Dynamic Link](plugins/dynamiclink/README.md) | [AGENTS.md](plugins/dynamiclink/AGENTS.md) |
| [Partner Links](plugins/partnerlinks/README.md) | [AGENTS.md](plugins/partnerlinks/AGENTS.md) |
| [Push](plugins/push/README.md) | [AGENTS.md](plugins/push/AGENTS.md) |
| [MCP](plugins/mcp/README.md) | [AGENTS.md](plugins/mcp/AGENTS.md) |
| [Schema Lock](plugins/schemalock/README.md) | [AGENTS.md](plugins/schemalock/AGENTS.md) |
| [Currency Rates](plugins/currencyrates/README.md) | [AGENTS.md](plugins/currencyrates/AGENTS.md) |

В example коллекция **demo_homepage** показывает Singleton: форму вместо таблицы и шесть наборов контента. Включение Singleton задаётся в миграции; в приложении без Schema Lock также доступен переключатель **Collection settings → Singleton**.

Общие правила разработки — [AGENTS.md](AGENTS.md) и [CONTRIBUTING.md](CONTRIBUTING.md).
Актуальная готовность к публикации — [отчёт](docs/RELEASE_READINESS_2026-09-23.md); история ревью — [docs/REVIEW.md](docs/REVIEW.md).

## Проверки

```sh
GOTOOLCHAIN=auto go test -race ./...
GOTOOLCHAIN=auto go vet ./...
npm ci
npx playwright install chromium
npm run test:browser
npm run test:docker
```

Браузерные тесты запускают два стенда с временными БД: `tests/testapp` без ограничений на `127.0.0.1:8097` для редакторов плагинов и защищённый example на `127.0.0.1:8099` для Schema Lock. После завершения данные удаляются. Проверяются обе темы, CRUD, связи, Singleton, Variants и отсутствие запрещённых действий. Скриншоты находятся в `test-results/`.

Docker smoke-тест использует собственный Compose project, порт `8098` (переопределяется `POCKETBASE_TEST_PORT`) и временный volume. Он проверяет сборку, healthcheck, embedded UI, сохранность изменений и отсутствие повторного seed после рестарта, затем удаляет только созданные им ресурсы.

## Партнёрские ссылки и Flutter

В example подключён Partner Links: **Партнёрские ссылки** в верхней панели, коллекция `partner_links` и демонстрационный провайдер. Для выдачи ссылок задайте Application ID/Post API key в админке, AppMetrica profileId всегда равен `user.id`. Реальные ключи не входят в демо. [Настройка и контракт API](plugins/partnerlinks/README.md).

Пакеты [dynamic_link](packages/dynamic_link), [dynamic_link_flutter](packages/dynamic_link_flutter) и [partner_links_flutter](packages/partner_links_flutter) находятся в `packages/`. Требуется Dart ≥3.13.2; пример интеграции и команды тестирования — в [README клиента](packages/partner_links_flutter/README.md).


## Flutter workspace и запускаемый пример

`.fvmrc` закрепляет стабильный Flutter **3.47.5** (Dart **3.13.4**). Корневой `pubspec.yaml` объединяет шесть пакетов и приложение; зависимости фиксируются единым `pubspec.lock`. Исходный workspace `mfo_hub` не используется.

[app_messaging_flutter](packages/app_messaging_flutter/README.md) объединяет Firebase Core/Messaging и AppMetrica Analytics/Push с обязательным callback нажатия. [push_links_flutter](packages/push_links_flutter/README.md) предоставляет callback для маршрутов и партнёрских ссылок: успешная ссылка открывается в WebView поверх приложения, неудачный resolve не меняет экран.

В VS Code открывайте корень `pocket_mfo`: `.vscode/settings.json` выбирает Flutter и Dart из `.fvm/flutter_sdk`, переопределяя глобальный SDK редактора. После первого `fvm install` или смены SDK выполните **Developer: Reload Window**. Зависимости получайте через `fvm flutter pub get`; обычная команда `flutter` может использовать другую глобальную версию.

```sh
fvm install
fvm flutter pub get
cd examples/partner_links_app
fvm flutter run -d chrome
```

Запустите Go example отдельно на порту 8090. Пример показывает вход, офферы, открытие свежей ссылки и свои заказы. [Инструкция и настройка сервера](examples/partner_links_app/README.md).

Dynamic Link подключается отдельным плагином: `dynamiclink.Register(app)` после Variants/Singleton. Миграция example создаёт `dynamic_link_settings` — Singleton с Variants для общих режима и предупреждения. Partner Links всегда создаёт `conversations`: запись появляется при выдаче ссылки, обновляется постбеками и доступна владельцу. Без заявки она хранится 14 дней, с заявкой — бессрочно; сроки меняются в настройках. Revenue включается у провайдера отдельно; [настройка аналитики](plugins/partnerlinks/README.md#доходы-и-воронка-в-appmetrica).

Проверки Flutter из корня:

```sh
fvm flutter analyze
fvm dart test packages/dynamic_link/test
fvm flutter test packages/pocket_mfo_flutter/test packages/dynamic_link_flutter/test packages/partner_links_flutter/test packages/app_messaging_flutter/test packages/push_links_flutter/test examples/partner_links_app/test
```

## Единый Flutter-пакет

В example также подключены вкладки **Пуши** и **MCP**. MCP доступен по `/api/mcp`; ключ создаётся в админке с отдельными правами на инструменты и коллекции. `push_launch` разрешается отдельно от подготовки кампаний. По умолчанию MCP-контент ограничен `demo_offers`, `partner_links`, `demo_screen_configs`, `dynamic_link_settings`. Для рассылок нужны OAuth token AppMetrica в серверных настройках и регистрация нативных устройств; сохранение кампании ничего не отправляет. [Подключение MCP и интерфейс провайдера](plugins/mcp/README.md), [кампании и атрибуция](plugins/push/README.md).

[pocket_mfo_flutter](packages/pocket_mfo_flutter/README.md) объединяет гостевой/обычный вход, защищённое хранение сессии, AppMetrica с user.id, снимок экспериментов в каждом событии, партнёрские ссылки и опциональные пуши. [Пример](examples/partner_links_app) автоматически входит гостем через `users`. Существующая `demo_members` переименовывается с сохранением ID и записей; заполненная отдельная `users` требует ручного решения конфликта, миграция её не удаляет.
