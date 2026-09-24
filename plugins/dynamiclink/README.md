# Dynamic Link

Переиспользуемый тип поля `dynamicLink` для PocketBase v0.40.4 / Go 1.27. Он объединяет URL и параметры открытия в одно проверяемое значение. В поле отображается URL и сворачиваемая настройка категории и заголовка WebView. Общие параметры и категории доступны через Dynamic Link в верхней панели; редактировать JSON не требуется.

## Подключение

```go
import "github.com/NikitaVasin/pocket_mfo/plugins/dynamiclink"

dynamiclink.Register(app) // до Bootstrap/Start
```

Регистрируйте плагин явно, в том числе при использовании Partner Links. Для своего поля добавьте его в любую обычную или auth-коллекцию миграцией/Go-кодом:

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
  "mode": "browser",
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
| `mode` | `appView` — WebView; `view` — браузер ОС внутри приложения; `browser` — внешний браузер | `browser` |
| `saveCooke` | Сохранение cookies (имя сохранено для совместимости) | `true` |
| `showLoader` | Индикатор загрузки WebView | `true` |
| `changeClient` | Альтернативный User-Agent адаптера, применяется при включённых cookies | `false` |
| `openUrlsInBrowser` | Последующие ссылки со страницы во внешнем браузере | `false` |
| `skipWarningDialog` | Пропуск предупреждения | `false` |
| `category` | Код категории из общих настроек | Пусто: общие настройки |
| `title`, `trackName` | Заголовок и имя для аналитики | Пустые |
| `warningDialog` | Свой текст подтверждения; нужны и title, и content | Не задан |

Неизвестные параметры, неверные типы, неподдерживаемая схема URL или режим отклоняются сервером. Скрытие/Required/Help поля работают через стандартные настройки PocketBase.

Поле само не открывает URL и не отправляет аналитику. Во Flutter поля значения можно передать конструктору `DynamicLink(...)` из `packages/dynamic_link` и открыть ссылку существующим адаптером `dynamic_link_flutter`. Partner Links использует это же значение в ответе resolve, заменяя исходный URL публичной ссылкой со случайным токеном.

Проверки: `go test -race ./plugins/dynamiclink ./plugins/partnerlinks`, браузерные `dynamiclink.spec.js` и `partnerlinks.spec.js`.

## Общие настройки по Variants

Зарегистрируйте `variants.Register(app)`, `singleton.Register(app)`, затем `dynamiclink.Register(app)` до Bootstrap/Start. В миграции после создания auth-коллекции вызовите:

```go
return dynamiclink.Configure(app, "users")
```

Это атомарно создаёт `dynamic_link_settings`: одну запись на набор Variants, с исходным набором Default. Повторный Configure добавляет недостающие поля категорий и сохраняет данные. В существующем приложении вызовите Configure новой миграцией; example содержит такую миграцию. Политика относится к одной auth-коллекции; для остальных пользователей и суперпользователя настройки самой ссылки сохраняются.

В разделе **Dynamic Link** верхней панели доступны штатные Singleton-форма и Variants: создайте нужные аудитории/эксперименты и заполните запись каждого набора. Пустой набор не наследует Default и открывает ссылки в браузере. Общие параметры применяются и к ранее сохранённым ссылкам; индивидуальные параметры открытия больше не определяют их поведение.

| Поле | Поведение |
| --- | --- |
| mode | Пусто или browser: внешний браузер; appView: WebView; view: браузер ОС внутри приложения |
| warningPolicy | inherit/disabled: без предупреждения; replace: общее предупреждение |
| warningTitle / warningContent | Обязательны для replace |
| openingOptions | Общие saveCooke, showLoader, changeClient, openUrlsInBrowser; необязательные bool, включая явный false |
| categories | До 100 категорий: key, label, options. options переопределяет mode, флаги и warningPolicy/Title/Content |

Сервер применяет выбранную политику к полям dynamicLink в Records API, включая expand, и в пользовательском realtime-enrich. Исходные записи в базе не меняются. Ответ суперпользователю остаётся исходным для редактирования. Гостям переопределения не применяются. После изменения аудитории клиент должен заново загрузить ссылки; уже полученный объект не меняется автоматически.

В собственном Go endpoint используйте `dynamiclink.Apply(app, authenticatedRecord, value)`. Partner Links вызывает его при resolve. Регистрируйте оба плагина явно: Partner Links не регистрирует Dynamic Link за приложение.

Категория выбирается из сохранённых категорий всех наборов. В ответе применяется категория с тем же `key` из выбранного пользователю набора; отсутствующая или удалённая категория использует общие настройки этого набора. Пустое значение категории означает общие настройки. Код: 1–64 латинских букв, цифр, `_` и `-`; внутри набора коды уникальны. Переименование label сохраняет связи; при изменении key ссылки нужно переназначить.

В поле ссылки заголовок WebView всегда доступен внутри раскрываемого блока; он используется в режиме appView. Параметры открытия, cookies, альтернативный User-Agent адаптера, загрузка и предупреждение редактируются только в общих настройках/категории. URL и title сохраняются в исходной ссылке; category также передаётся в Dart `DynamicLink`.

Общие настройки открываются через Dynamic Link в верхней панели; `dynamic_link_settings` скрыта из бокового списка Collections. Булевы параметры открытия редактируются свитчами; у категории «Сбросить» возвращает отдельный параметр к общему значению. Удаление записей настроек, коллекции и truncate через HTTP запрещены, включая суперпользователей и batch; доверенный Go-код сохраняет доступ.
