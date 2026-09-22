# Dynamic Link

Переиспользуемый тип поля `dynamicLink` для PocketBase v0.40.4 / Go 1.27. Он объединяет URL и параметры открытия в одно проверяемое значение. В админке отображаются URL, список режимов, переключатели и текстовые поля; редактировать JSON не требуется.

## Подключение

```go
import "github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"

dynamiclink.Register(app) // до Bootstrap/Start
```

Partner Links подключает этот плагин автоматически. При отдельном использовании добавьте поле в любую обычную или auth-коллекцию миграцией/Go-кодом:

```go
banners.Fields.Add(&dynamiclink.Field{
    JSONField: core.JSONField{
        Name: "destination",
        Required: true,
        Help: "Ссылка баннера и параметры её открытия",
    },
})
if err := app.Save(banners); err != nil { return err }
record.Set("destination", map[string]any{"url": "https://example.com/banner"})
if err := app.Save(record); err != nil { return err }
```

Без Schema Lock тип доступен через **Collection settings → New field → Dynamic link**. При Schema Lock схема меняется кодом, но обычный редактор записи работает. Требования доступа задаются штатными API rules коллекции. Новых HTTP-маршрутов нет.

## Значение

В БД и Records API поле представлено объектом с фиксированным контрактом:

```json
{
  "url": "https://example.com/banner",
  "mode": "appView",
  "saveCooke": true,
  "showLoader": true,
  "changeClient": false,
  "openUrlsInBrowser": false,
  "skipWarningDialog": false,
  "title": "Предложение",
  "trackName": "banner",
  "warningDialog": {"title": "Продолжить?", "content": "Переход на сайт партнёра"}
}
```

Достаточно передать `url`; пропущенные параметры открытия заполняются и сохраняются по умолчанию. Явный `false` сохраняется. `null` допустим для необязательного поля и `warningDialog`. У обязательного поля URL должен быть заполнен. Максимум — 16384 байта на значение.

| Параметр | Назначение | По умолчанию |
| --- | --- | --- |
| `url` | HTTP(S) URL без логина/пароля | Обязателен для непустого значения |
| `mode` | `appView` — WebView; `view` — браузер ОС внутри приложения; `browser` — внешний браузер | `appView` |
| `saveCooke` | Сохранение cookies (имя сохранено для совместимости) | `true` |
| `showLoader` | Индикатор загрузки WebView | `true` |
| `changeClient` | Альтернативный User-Agent адаптера, применяется при включённых cookies | `false` |
| `openUrlsInBrowser` | Последующие ссылки со страницы во внешнем браузере | `false` |
| `skipWarningDialog` | Пропуск предупреждения | `false` |
| `title`, `trackName` | Заголовок и имя для аналитики | Пустые |
| `warningDialog` | Свой текст подтверждения; нужны и title, и content | Не задан |

Неизвестные параметры, неверные типы, неподдерживаемая схема URL или режим отклоняются сервером. Скрытие/Required/Help поля работают через стандартные настройки PocketBase.

Поле само не открывает URL и не отправляет аналитику. Во Flutter поля значения можно передать конструктору `DynamicLink(...)` из `packages/dynamic_link` и открыть ссылку существующим адаптером `dynamic_link_flutter`. Partner Links использует это же значение в ответе resolve, заменяя исходный URL публичной зашифрованной ссылкой.

Проверки: `go test -race ./plugins/dynamiclink ./plugins/partnerlinks`, браузерные `dynamiclink.spec.js` и `partnerlinks.spec.js`.

## Общие настройки по Variants

Зарегистрируйте `variants.Register(app)`, `singleton.Register(app)`, затем `dynamiclink.Register(app)` до Bootstrap/Start. В миграции после создания auth-коллекции вызовите:

```go
return dynamiclink.Configure(app, "users")
```

Это атомарно создаёт `dynamic_link_settings`: одну запись на набор Variants, с исходным набором Default. Повторный Configure сохраняет данные. Политика относится к одной auth-коллекции; для остальных пользователей и суперпользователя настройки самой ссылки сохраняются.

В коллекции доступны штатные Singleton-форма и Variants: создайте нужные аудитории/эксперименты и заполните запись каждого набора. Пустой набор не наследует Default.

| Поле | Поведение |
| --- | --- |
| mode | Пусто: режим ссылки. appView/view/browser: переопределить все ссылки пользователя |
| warningPolicy | inherit: предупреждение ссылки; disabled: отключить; replace: общее предупреждение |
| warningTitle / warningContent | Обязательны для replace; общее предупреждение показывается даже при skipWarningDialog у ссылки |

Сервер применяет выбранную политику к полям dynamicLink в Records API, включая expand, и в пользовательском realtime-enrich. Исходные записи в базе не меняются. Ответ суперпользователю остаётся исходным для редактирования. Гостям переопределения не применяются. После изменения аудитории клиент должен заново загрузить ссылки; уже полученный объект не меняется автоматически.

В собственном Go endpoint используйте `dynamiclink.Apply(app, authenticatedRecord, value)`. Partner Links вызывает его при resolve. Регистрируйте оба плагина явно: Partner Links не регистрирует Dynamic Link за приложение.
