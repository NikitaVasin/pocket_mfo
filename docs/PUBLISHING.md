# Публикация и подключение

Репозиторий: [NikitaVasin/pocket_mfo](https://github.com/NikitaVasin/pocket_mfo). Один Go-модуль `github.com/NikitaVasin/pocket_mfo` содержит четыре пакета плагинов. У них общая версия; отдельные go.mod внутри plugins не нужны. `.git` используется в Git URL, но не в module path или Go-импортах.

## Подготовка первого выпуска

1. Выполните проверки из [CONTRIBUTING.md](../CONTRIBUTING.md). Просмотрите `git status` и diff; не включайте базы, бэкапы и пользовательские секреты. Example содержит только намеренно опубликованные демопароли.
2. Если remote ещё не настроен, добавьте `origin` с адресом `https://github.com/NikitaVasin/pocket_mfo.git` или `git@github.com:NikitaVasin/pocket_mfo.git`. Перед отправкой проверьте `git remote -v` и существующую историю удалённой ветки. Не перезаписывайте её через force push.
3. Отправьте согласованный коммит и дождитесь GitHub Actions. Workflow выполняет проверки, но ничего не публикует и не создаёт теги автоматически.
4. Выберите версию первого выпуска и создайте обычный SemVer-тег `v0.x.y` на проверенном коммите. После тега подключайте потребителей к этой версии. До первого тега Go поддерживает получение по commit и `@latest`.
5. Для следующих выпусков описывайте изменения контракта, совместимую версию PocketBase и необходимые миграции. Мажорная версия v2 потребует суффикса `/v2` в пути Go-модуля и импортах.

Лицензия пока не выбрана и по решению владельца не добавлена. Публичность исходников сама по себе не задаёт условия свободного использования; не указывайте вымышленную лицензию в README или метаданных выпуска.

## Подключение к приложению

После отправки исходников в GitHub:

```sh
go get github.com/NikitaVasin/pocket_mfo@latest
```

В production фиксируйте конкретный тег или commit в go.mod/go.sum. Импортируйте нужный пакет, например:

```go
import "github.com/NikitaVasin/pocket_mfo/plugins/schemalock"

// До app.Bootstrap / app.Start.
schemalock.Register(app)
```

Остальные пакеты: `plugins/polymorphicrelation`, `plugins/variants`, `plugins/singleton`. Их Register вызываются отдельно; Schema Lock не устанавливает автоматически Variants. Не импортируйте `example/migrations` в рабочее приложение: это демонстрационные данные и учётные записи.

Для соседнего локального checkout:

```go
require github.com/NikitaVasin/pocket_mfo v0.0.0

replace github.com/NikitaVasin/pocket_mfo => ../pocket_mfo
```

Это фрагмент go.mod потребителя. Перед обычной сборкой из GitHub уберите локальный replace и выберите опубликованную версию.

## Если репозиторий станет приватным

Пути пакетов остаются теми же. Пользователь или CI, собирающий потребителя, должен иметь право чтения репозитория и настроенную Git-аутентификацию. Задайте `GOPRIVATE=github.com/NikitaVasin/pocket_mfo` в окружении сборки; если GOPRIVATE уже содержит другие шаблоны, добавьте новый через запятую, сохранив прежние.

Для разовой команды без постоянного изменения настроек:

```sh
GOPRIVATE="${GOPRIVATE:+$GOPRIVATE,}github.com/NikitaVasin/pocket_mfo" \
  go get github.com/NikitaVasin/pocket_mfo@latest
```

`GOPRIVATE` исключает модуль из публичного proxy/checksum database; доступ к Git оно не предоставляет. Используйте SSH-ключ с доступом к репозиторию либо HTTPS credential helper. Секреты храните в менеджере credentials или secrets CI, не в module path, исходниках и workflow. Стандартный `GITHUB_TOKEN` другого репозитория не даёт автоматического доступа к этому приватному модулю.

Подробности: [приватные модули Go](https://go.dev/ref/mod#private-modules).
