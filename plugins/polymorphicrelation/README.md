# Polymorphic Relation

Плагин для PocketBase **v0.40.4**: одна ссылка на одну запись из нескольких разрешённых коллекций. Подходит для комментариев к статьям и видео, вложений к разным типам сущностей. Поддерживает нативный relation picker, API rules, expand и realtime.

## Подключение

```go
import (
    "github.com/pocketbase/pocketbase"
    "github.com/NikitaVasin/pocket_mfo/plugins/polymorphicrelation"
)

app := pocketbase.New()
polymorphicrelation.Register(app) // до Bootstrap / Start
if err := app.Start(); err != nil {
    panic(err)
}
```

Пакет входит в модуль `github.com/NikitaVasin/pocket_mfo`. Установка и локальное подключение описаны в [общем README](../../README.md#подключение).

UI встроен в Go-бинарник через `embed.FS`; отдельная сборка frontend не нужна. При включённом Schema Lock поле создаётся только из Go/миграций; редактор схемы в UI недоступен.

Создание поля из Go:

```go
comments.Fields.Add(&polymorphicrelation.Field{
    JSONField: core.JSONField{
        Name: "subject",
        Required: false,
        Help: "Статья или видео, к которому относится комментарий",
    },
    CollectionIDs: []string{articles.Id, videos.Id}, // именно ID коллекций
    OnDelete: polymorphicrelation.Restrict,
})
err := app.Save(comments)
```

В админке: **Collection settings → New field → Polymorphic relation**. Выберите коллекции; в настройках поля задайте обязательность и поведение при удалении. В форме записи сначала выбирается коллекция, затем одна запись через стандартный picker PocketBase.

## Значения и целостность

```json
{"subject":{"collectionId":"demoarticles001","recordId":"RECORD_ID"}}
```

Пустое значение — `null`. Отсутствие `subject` в PATCH сохраняет текущую связь; явный `null` очищает её, если поле необязательно. Массив ссылок не поддерживается. Один родитель может использоваться любым числом дочерних записей.

Для каждого разрешённого типа плагин создаёт штатный `core.RelationField` с `MaxSelect=1` и обычный индекс. Публичный JSON и служебные столбцы сохраняются в одной транзакции; только столбец выбранной коллекции содержит ID. Смена типа очищает предыдущий столбец.

| `onDelete` | Поведение |
| --- | --- |
| `restrict` (по умолчанию) | Удаление родителя блокируется, пока есть дочерние записи |
| `setNull` | Все соответствующие необязательные поля дочерних записей очищаются |
| `cascade` | PocketBase удаляет связанные дочерние записи |

`setNull` несовместим с `required`. При ошибке в любой части удаления транзакция откатывается, включая уже очищенные связи. Сервисные записи через Records API запрещены, в том числе с модификаторами `+`/`-`. Программные сохранения через `app.Save` и `app.SaveNoValidate` также синхронизируют и проверяют полиморфные значения.

Схема поддерживает обычные и auth-коллекции. Системные коллекции и views исключены. Добавление типа создаёт столбец; удаление используемого типа блокируется. Переименование коллекции или поля сохраняет связи благодаря стабильным ID. Удаление самого полиморфного поля удаляет и его столбцы/индексы. Удаление целевой коллекции блокируется штатными ссылками схемы, пока тип присутствует в настройках поля.

Строгие отношения обеспечиваются штатными механизмами PocketBase, не SQL `FOREIGN KEY`. Прямой SQL в обход модели не является поддерживаемым способом записи.

## Records API, expand и правила

Работают стандартные `/api/collections/{collection}/records`, без отдельных endpoint для связей:

```http
PATCH /api/collections/comments/records/COMMENT_ID
Content-Type: application/json
Authorization: YOUR_TOKEN

{"subject":{"collectionId":"demovideos00001","recordId":"VIDEO_ID"}}
```

```http
GET /api/collections/comments/records?expand=subject
```

В `subject` остаётся объект ссылки; `expand.subject` содержит родительскую запись. Если родитель недоступен по `ViewRule`, раскрытого значения нет. Скрытые поля, email и прочие auth-атрибуты обрабатываются штатными правилами PocketBase. Само наличие ссылки не требует права просмотра родителя — как у обычного relation; ограничения назначения задаются API rules дочерней коллекции.

Служебные значения отсутствуют в HTTP-ответах и realtime. UI скрывает служебные поля и индексы. Серверный экспорт схемы содержит их; импорт восстанавливает тип поля и связи. UI-экспорт может не содержать автоматически создаваемые поля — при импорте плагин восстановит их по ID.

Фильтры, API rules и обратные связи используют реальные имена relation-столбцов. Получить имя можно из `relations` в настройках полиморфного поля в API схемы либо в Go:

```go
name := polymorphicrelation.ServiceFieldName(field.Id, articles.Id)
```

Имя имеет вид `pmr_<24 hex>`: первые 12 байт SHA-256 от `fieldId + "\x00" + collectionId`. Для примера `subject00000001` → `demoarticles001` это `pmr_ded421973646b41268075007`.

Примеры с этим именем:

```text
filter:     pmr_ded421973646b41268075007 = "ARTICLE_ID"
CreateRule: pmr_ded421973646b41268075007.title != ""
ListRule:   pmr_ded421973646b41268075007 != ""
```

На статье обратное раскрытие: `expand=comments_via_pmr_ded421973646b41268075007`. `UpdateRule` по обычному relation проверяет существующую запись; ограничения нового значения можно выразить через `@request.body.subject.collectionId` и `@request.body.subject.recordId`. Скрытое полиморфное поле делает скрытыми и его служебные relations, сохраняя обычные ограничения публичной фильтрации PocketBase.

Публичный `expand=subject` поддерживается в Records API, ответах авторизации и realtime. Вложенные переходы через несколько полиморфных полей и универсальный синтаксис `filter=subject.title…` в первую версию не входят. В Go `app.ExpandRecord` используйте со служебным именем; публичный alias обрабатывается на уровне API enrichment.

## Dart SDK

```dart
import 'package:pocketbase/pocketbase.dart';

final pb = PocketBase('http://127.0.0.1:8090');
// Для записи предварительно авторизуйтесь пользователем с подходящими API rules.
final article = await pb.collection('articles').getFirstListItem('');
final comment = await pb.collection('comments').create(
  body: {
    'text': 'Комментарий из Dart',
    'subject': {
      'collectionId': article.collectionId,
      'recordId': article.id,
    },
  },
  expand: 'subject',
);

final reference = comment.data['subject'] as Map<String, dynamic>?;
final parent = comment.expand['subject']?.firstOrNull;

await pb.collection('comments').update(comment.id, body: {'subject': null});

final unsubscribe = await pb.collection('comments').subscribe(
  '*',
  (event) => print(event.record?.data['subject']),
  expand: 'subject',
);
await unsubscribe();
```

