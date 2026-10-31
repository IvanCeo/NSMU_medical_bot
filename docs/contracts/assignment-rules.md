# Контракт: новые задания и список «Все»

Статус: утверждён пользователем 2026-10-31 вместе с
[планом](../plans/2026-10-30-assignment-rules.md).

## Доменные изменения

- DeliveryKind получает AssignmentCreatedDelivery = "assignment_created".
- DeliveryDraft получает AssignmentID *int64. Поле обязательно только для
  assignment_created; у reminder/digest остаётся NULL.
- Outgoing получает EventKind DeliveryKind, чтобы transport выбирал русский
  заголовок уведомления независимо от адресата Kind=private/group. Пустое
  EventKind сохраняет прежний выбор заголовка для совместимости адаптеров/тестов.
- AssignmentFilter получает After *time.Time — строгую нижнюю границу дедлайна
  для repository. Service очищает внешние временные границы и вычисляет их сам.

## Репозиторные порты

DeliveryRepo сохраняет прежние методы и дополнительно получает:

```go
CancelRemindersForAssignment(ctx context.Context, assignmentID int64, at time.Time) error
CancelRemindersForUser(ctx context.Context, userID int64, assignmentID *int64, at time.Time) error
```

Они отменяют только reminder. CancelForAssignment теперь отменяет reminder
и assignment_created этого задания, сохраняя групповой digest. CancelForUser
отменяет оба личных типа, при assignmentID != nil только выбранного задания.
CancelForGroup по-прежнему относится только к digest.

Правка задания и включённых настроек перепланирует только reminder; ожидающее
assignment_created сохраняется и перепроверяется при отправке. Done, отмена,
выход, отключение уведомлений отменяют оба типа. Undo перепланирует только
будущие reminder. У 403 текущая отправка сначала завершается, затем отменяются
остальные личные отправки, чтобы не нарушить CAS lease.

## Очередь

Outbox получает assignment_id NULL REFERENCES assignments(id). Новый CHECK
для assignment_created требует assignment_id и user_id, запрещает reminder_id
и digest_run_id; part=0. Прежние комбинации сохраняются. Новые индексы:
outbox(assignment_id) WHERE kind='assignment_created' и outbox(reminder_id)
WHERE kind='reminder'. Прежние индексы после пересоздания восстанавливаются.

EventKey: assignment_created:<assignmentID>:<userID>. Payload содержит один ID
задания; текст строится перед отправкой по актуальным данным. Уведомление
не отменяется только из-за смены version. NextAttemptAt — время публикации
либо конец тихих часов. ExpiresAt — дедлайн, зафиксированный при публикации;
при отправке также проверяется актуальный дедлайн. Продление срока не продлевает
срок жизни старого оповещения. Задания, созданные до миграции, не backfill-ятся.

Получатели фиксируются при публикации: текущие участники, включая автора при
членстве, PrivateChatAvailable=true и Preferences.RemindersEnabled=true.
Тот же флаг управляет обоими личными типами; UI называет его «Личные уведомления».
Перед HTTP заново проверяются членство, доступность чата, настройка, тихие часы,
active, отсутствие личной отметки и будущий дедлайн. Кнопка открывает карточку
через прежний view:<ID>; групповая доставка новых заданий не добавляется.

Lease, пять попыток, retry_after, идемпотентное планирование, 403 и граница
возможного дубля между Telegram accept и DB ack остаются прежними.

## Список «Все» и transport

Service.Tasks при Period пустом или all задаёт After=now и DoneFilter=pending.
Пустой/all/pending DoneFilter принимаются; all+done возвращает ErrInvalidInput.
Репозиторий применяет deadline_at>After и личную отметку до LIMIT и курсора.
Прямые внутренние запросы для дайджеста/планирования продолжают использовать
свои явные границы, не наследуя правило пользовательского списка.

Названия кнопок — нормализованные темы заданий, до 60 символов с многоточием;
при одинаковом коротком представлении добавляется ID. OutgoingButton.Label
передаёт тему, а не прежний ключ open_task; старый ключ адаптер продолжает
поддерживать. Идентификатор остаётся только в callback/deep link и в карточке.

Новых команд Telegram, callback-форматов, env-ключей приложения и публичных HTTP-эндпоинтов нет.

## Резервное копирование (утверждённое уточнение)

Один публикуемый образ содержит `/app/bot` и `/app/backup`. Профиль Compose
`maintenance`, сервис `backup`, entrypoint `/app/backup`. Никакого Go/Make на VPS.
CLI: `create --source /data --destination /backups`, `verify --archive PATH`,
`restore --archive PATH --destination /restore` (только пустой каталог).
По умолчанию запускается create. Архив tar.gz включает всё содержимое data,
не допускает symlink/special files; verify читает архив полностью. Restore
проверяет имена и содержимое до записи, не перезаписывает файлы и восстанавливает
права доступа для UID/GID 65532 контейнера бота. CLI не открывает SQLite, не применяет
миграции, не читает .env и не управляет контейнерами. Бот останавливается оператором.
Серверный пример использует только image; BOT_IMAGE — переменная подстановки
Compose, не новый ключ конфигурации приложения. Локальный `make backup` — обёртка
только над однократным create; остановка и запуск отдельными явными командами.
