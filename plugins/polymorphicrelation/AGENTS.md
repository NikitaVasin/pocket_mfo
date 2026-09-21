# Polymorphic Relation: инструкция для агента

Используйте для связи с **одним** родителем из нескольких коллекций: например, комментарий относится к статье либо видео. Для нескольких родителей нужен другой контракт; этот плагин не хранит массив ссылок. Пользовательская документация — [README.md](README.md).

## Как подключать

1. Вызовите `polymorphicrelation.Register(app)` до запуска PocketBase.
2. Сначала сохраните целевые коллекции, затем добавьте `Field` в дочернюю коллекцию через Go/миграцию. В `CollectionIDs` передаются ID, не названия.
3. Записывайте публичное поле через обычный Records API или `app.Save`:

```go
comments.Fields.Add(&polymorphicrelation.Field{
    JSONField: core.JSONField{Name: "subject"},
    CollectionIDs: []string{articles.Id, videos.Id},
    OnDelete: polymorphicrelation.Restrict,
})
if err := app.Save(comments); err != nil {
    return err
}
```

Значение: `{"collectionId":"...","recordId":"..."}`, отсутствие связи — `null`. `expand=subject` раскрывает родителя с учётом его прав доступа. Подробный Dart-пример есть в README.

## Инварианты и сопровождение

- Плагин создаёт реальные relation-поля `pmr_*` и индексы. Не задавайте их вручную и не разрешайте их запись через HTTP, включая модификаторы `+`/`-`.
- Для фильтров, правил и обратных связей используйте `ServiceFieldName(fieldID, collectionID)`, а не вычисление имени на стороне клиента. Публичный alias `subject` поддерживается для API expand; Go `app.ExpandRecord` использует служебное имя.
- `Restrict` запрещает удаление используемого родителя; `SetNull` очищает необязательные связи; `Cascade` удаляет дочерние записи. `SetNull` нельзя сочетать с `Required`.
- Изменения публичного JSON и служебных relations должны оставаться согласованными при Save, SaveNoValidate, удалении и откате. Переименование не должно ломать связь: идентичность задают ID.
- Поле разрешено в обычных и auth-коллекциях, исключены системные и view. Schema Lock запрещает создание/изменение поля через UI; picker в форме записи продолжает работать.
- Основные файлы: `field.go` — тип/валидация, `schema.go` — companions/индексы, `plugin.go` — хуки целостности, `enrich.go` — HTTP/auth/realtime expand.

Проверки: `go test -race ./plugins/polymorphicrelation` и `npx playwright test --project=plugins polymorphic.spec.js`. При изменении совместных хуков выполните проверки из корневого AGENTS.md. Сохраняйте тесты импорта, циклов cascade, отката setNull, auth/realtime enrichment и столкновения ID.
