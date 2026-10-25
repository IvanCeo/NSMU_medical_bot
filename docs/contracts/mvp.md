# Контракт сервисов полного MVP

Статус: утверждено пользователем 25.10.2026 вместе с
[планом MVP](../plans/2026-10-25-mvp.md). Это спецификация, не реализация.

## Доменные типы

Никакие типы Telegram SDK или database/sql не выходят в сервисный контракт.
Все I/O-методы принимают context первым аргументом. Время доменных объектов —
time.Time, идентификаторы — int64, роль/вид/статус — закрытые строковые enum.

- User: текущие id, telegram_id, display_name, group_id?, role, created_at;
  дополнительно PrivateChatAvailable bool.
- Group: текущие поля + TelegramChatID?, DigestEnabled bool, Version int64,
  ChatBindingVersion int64.
- Profile: User, Group?, Preferences; без кода приглашения для student.
- Subject: поля из плана, включая Version, ArchivedAt?.
- Assignment: поля из плана, Version и []Attachment.
- AssignmentView: Assignment + Done bool для текущего пользователя.
- Reminder, Delivery, DigestRun, Dialog, AuditEntry: типизированное представление
  соответствующих таблиц из плана.
- Preferences: RemindersEnabled bool и QuietHours? с Start/End в HH:MM.
- PageRequest: Cursor string, Limit int (по умолчанию 10, максимум 20).
  Страницы содержат Items и NextCursor; задание сортируется по deadline_at и id.
- AssignmentFilter: GroupID, SubjectID?, Period day/week/all/overdue,
  DoneFilter all/pending/done. Личный UserID задаётся сервисом, не клиентом.
- AssignmentInput: GroupID, SubjectID, Kind, Title, Description, DeadlineAt,
  Importance, URL?, []AttachmentInput.
- AssignmentPatch: те же изменяемые поля как указатели; отсутствие поля
  означает «сохранить», пустое описание/ссылка — «очистить».
- AttachmentInput: Kind document/photo, FileID, FileName, MIME.
- SubjectInput: GroupID, Name, Teacher?; SubjectPatch: Name?, Teacher?, Archived?.
- GroupPatch: Title?, DigestEnabled?.
- ReminderPlan: AssignmentID, UserID, AssignmentVersion, Source, Offset?, SendAt.
- Dialog: ID, UserID, Kind, Step, Payload, Revision, ExpiresAt, UpdatedAt.
- DialogInput: Text? / Selection? / Attachment?; одновременно только один вид.
- DialogView: ID, Revision, Step, PromptKey, []Option, Preview? — бот переводит
  это в русские сообщения и клавиатуры.
- DeliveryDraft: EventKey, Kind, ReminderID? / DigestRunID?, GroupID, UserID?,
  Part, Payload с ID и version, NextAttemptAt, ExpiresAt.
- DeliveryFailure: код, Permanent bool, RetryAfter time.Duration; без URL/токена.
- DeliveryProgress: Total, Pending, Sent, Skipped, Failed, Cancelled — для
  завершения digest_run после отправки всех частей.
- ChatPermissions: ActorIsAdministrator, BotIsAdministrator, BotCanSend — bool.
- Outgoing: ChatID, Kind private/group, Text, типизированные кнопки со ссылкой
  на задание; без SDK-типов. MessageReceipt: ChatID, MessageID.

## Сервисы — вызываются обработчиками

ActorTelegramID всегда берётся из полученного от Telegram отправителя.
Доступ и выбор группы перепроверяются внутри метода.

```go
type UserCases interface {
    Register(ctx context.Context, user domain.TelegramUser) (domain.User, error)
    Profile(ctx context.Context, actorTelegramID int64) (domain.Profile, error)
    JoinGroup(ctx context.Context, actorTelegramID int64, inviteCode string) (domain.Profile, error)
    LeaveGroup(ctx context.Context, actorTelegramID int64) error
    Preferences(ctx context.Context, actorTelegramID int64) (domain.Preferences, error)
    SavePreferences(ctx context.Context, actorTelegramID int64, value domain.Preferences) error
}

type GroupCases interface {
    CreateGroup(ctx context.Context, actorTelegramID int64, title string) (domain.Group, error)
    Groups(ctx context.Context, actorTelegramID int64, page domain.PageRequest) (domain.GroupPage, error)
    UpdateGroup(ctx context.Context, actorTelegramID, groupID, version int64, patch domain.GroupPatch) (domain.Group, error)
    RotateInvite(ctx context.Context, actorTelegramID, groupID int64) (string, error)
    BindChat(ctx context.Context, actorTelegramID, groupID, chatID int64) error
    Members(ctx context.Context, actorTelegramID, groupID int64, page domain.PageRequest) (domain.UserPage, error)
    SetEditor(ctx context.Context, actorTelegramID, userID int64, editor bool) error
    RemoveMember(ctx context.Context, actorTelegramID, userID int64) error
}

type LearningCases interface {
    Subjects(ctx context.Context, actorTelegramID, groupID int64, includeArchived bool, page domain.PageRequest) (domain.SubjectPage, error)
    CreateSubject(ctx context.Context, actorTelegramID int64, input domain.SubjectInput) (domain.Subject, error)
    UpdateSubject(ctx context.Context, actorTelegramID, subjectID, version int64, patch domain.SubjectPatch) (domain.Subject, error)
    Tasks(ctx context.Context, actorTelegramID int64, filter domain.AssignmentFilter, page domain.PageRequest) (domain.AssignmentPage, error)
    Task(ctx context.Context, actorTelegramID, assignmentID int64) (domain.AssignmentView, error)
    CreateAssignment(ctx context.Context, actorTelegramID int64, input domain.AssignmentInput) (domain.Assignment, error)
    UpdateAssignment(ctx context.Context, actorTelegramID, assignmentID, version int64, patch domain.AssignmentPatch) (domain.Assignment, error)
    CancelAssignment(ctx context.Context, actorTelegramID, assignmentID, version int64) error
    SetComplete(ctx context.Context, actorTelegramID, assignmentID, version int64, done bool) (domain.AssignmentView, error)
    Snooze(ctx context.Context, actorTelegramID, assignmentID, version int64, delay time.Duration) error
    PreviewDigest(ctx context.Context, actorTelegramID, groupID int64, date time.Time) (domain.Digest, error)
    Audit(ctx context.Context, actorTelegramID, groupID int64, page domain.PageRequest) (domain.AuditPage, error)
}

type DialogCases interface {
    Start(ctx context.Context, actorTelegramID int64, kind domain.DialogKind, entityID int64) (domain.DialogView, error)
    Current(ctx context.Context, actorTelegramID int64) (*domain.DialogView, error)
    Apply(ctx context.Context, actorTelegramID, dialogID, revision int64, input domain.DialogInput) (domain.DialogView, error)
    Cancel(ctx context.Context, actorTelegramID int64) error
}
```

Apply завершает публикацию/изменение через сервисные сценарии, проверяя права
и версию ещё раз. Параллельные шаги защищены CAS; конечное сохранение и
завершение диалога атомарны через общую UnitOfWork.

## Репозитории — интерфейсы потребителя в internal/service

Существующие UpsertTelegramUser и SyncAdmins сохраняют сигнатуры.
Читающие методы возвращают ErrNotFound при отсутствии одиночной сущности,
а списки — пустую страницу. Ошибки ограничений и CAS переводятся в доменные ошибки.

```go
type UserRepo interface {
    UpsertTelegramUser(ctx context.Context, user domain.TelegramUser, createdAt time.Time) error
    ByTelegramID(ctx context.Context, telegramID int64) (domain.User, error)
    ByID(ctx context.Context, id int64) (domain.User, error)
    ListByGroup(ctx context.Context, groupID int64, page domain.PageRequest) (domain.UserPage, error)
    SetGroup(ctx context.Context, userID int64, groupID *int64) error
    SetRole(ctx context.Context, userID int64, role domain.Role) error
    SetPrivateChatAvailable(ctx context.Context, userID int64, available bool) error
}

type AdminRepo interface {
    SyncAdmins(ctx context.Context, telegramIDs []int64, createdAt time.Time) error
}

type GroupRepo interface {
    Create(ctx context.Context, group domain.Group) (domain.Group, error)
    ByID(ctx context.Context, id int64) (domain.Group, error)
    ByInviteCode(ctx context.Context, code string) (domain.Group, error)
    List(ctx context.Context, page domain.PageRequest) (domain.GroupPage, error)
    Update(ctx context.Context, group domain.Group, expectedVersion int64) error
}

type SubjectRepo interface {
    Create(ctx context.Context, subject domain.Subject) (domain.Subject, error)
    ByID(ctx context.Context, id int64) (domain.Subject, error)
    List(ctx context.Context, groupID int64, archived bool, page domain.PageRequest) (domain.SubjectPage, error)
    Update(ctx context.Context, subject domain.Subject, expectedVersion int64) error
}

type AssignmentRepo interface {
    Create(ctx context.Context, assignment domain.Assignment) (domain.Assignment, error)
    ByID(ctx context.Context, id int64) (domain.Assignment, error)
    List(ctx context.Context, filter domain.AssignmentFilter, viewerUserID int64, page domain.PageRequest) (domain.AssignmentPage, error)
    Update(ctx context.Context, assignment domain.Assignment, expectedVersion int64) error
}

type CompletionRepo interface {
    IsDone(ctx context.Context, userID, assignmentID int64) (bool, error)
    Set(ctx context.Context, userID, assignmentID int64, doneAt *time.Time) error
}

type ReminderRepo interface {
    ReplaceForAssignment(ctx context.Context, assignmentID int64, plans []domain.ReminderPlan, at time.Time) error
    CancelForUser(ctx context.Context, userID int64, assignmentID *int64, at time.Time) error
    Schedule(ctx context.Context, plans []domain.ReminderPlan) error
    Snooze(ctx context.Context, plan domain.ReminderPlan, at time.Time) error
    Due(ctx context.Context, now time.Time, limit int) ([]domain.Reminder, error)
    ByID(ctx context.Context, id int64) (domain.Reminder, error)
    MarkSent(ctx context.Context, id int64, at time.Time) error
    Reschedule(ctx context.Context, id int64, sendAt time.Time) error
    Cancel(ctx context.Context, id int64, at time.Time) error
}

type SettingsRepo interface {
    Get(ctx context.Context, userID int64) (domain.Preferences, error)
    Save(ctx context.Context, userID int64, preferences domain.Preferences) error
}

type DialogRepo interface {
    ByUser(ctx context.Context, userID int64) (*domain.Dialog, error)
    Save(ctx context.Context, dialog domain.Dialog, expectedRevision int64) (domain.Dialog, error)
    Finish(ctx context.Context, userID int64) error
    Expire(ctx context.Context, now time.Time) error
}

type DigestRepo interface {
    Ensure(ctx context.Context, groupID int64, localDate string, bindingVersion int64, at time.Time) (domain.DigestRun, bool, error)
    ByID(ctx context.Context, id int64) (domain.DigestRun, error)
    SetStatus(ctx context.Context, runID int64, status domain.DigestStatus) error
}

type DeliveryRepo interface {
    Enqueue(ctx context.Context, draft domain.DeliveryDraft) error
    Claim(ctx context.Context, now, leaseUntil time.Time, token string) (*domain.Delivery, error)
    Complete(ctx context.Context, id int64, token string, receipt domain.MessageReceipt, at time.Time) error
    Retry(ctx context.Context, id int64, token string, failure domain.DeliveryFailure, nextAt time.Time) error
    Fail(ctx context.Context, id int64, token string, status domain.DeliveryStatus, code string, at time.Time) error
    CancelForAssignment(ctx context.Context, assignmentID int64, at time.Time) error
    CancelForUser(ctx context.Context, userID int64, assignmentID *int64, at time.Time) error
    CancelForGroup(ctx context.Context, groupID int64, at time.Time) error
    Expire(ctx context.Context, now time.Time) error
    Progress(ctx context.Context, digestRunID int64) (domain.DeliveryProgress, error)
}

type AuditRepo interface {
    Append(ctx context.Context, entry domain.AuditEntry) error
    List(ctx context.Context, groupID int64, page domain.PageRequest) (domain.AuditPage, error)
}
```

Многошаговые изменения выполняются через UnitOfWork. Repositories содержит
UserRepo, GroupRepo, SubjectRepo, AssignmentRepo, CompletionRepo, ReminderRepo,
SettingsRepo, DialogRepo, DigestRepo, DeliveryRepo, AuditRepo.

```go
type UnitOfWork interface {
    WithinTx(ctx context.Context, fn func(Repositories) error) error
}
```

Внутри callback используются только переданные репозитории одной транзакции;
повторный вход в UnitOfWork и внешние сетевые вызовы запрещены.
Register вызывается для личных обновлений: помимо имени он отмечает доступность
личного чата. Привязка из группового чата использует уже существующую запись
администратора и не включает личную доставку без личного обращения к боту.
CancelForAssignment отменяет личные напоминания; части дайджеста перед отправкой
проверяют актуальность заданий из сохранённого списка ID, не отменяя весь
дайджест из-за изменения одного задания.

## Планировщик и внешние порты

Порты Telegram определяются потребителем в service и реализуются internal/bot.
Источник времени и генератор токенов передаются конструкторам. Новых глобальных
клиентов, ticker или изменяемых singleton не создаётся.

```go
type ChatVerifier interface {
    CheckBinding(ctx context.Context, chatID, actorTelegramID int64) (domain.ChatPermissions, error)
}

type NotificationSender interface {
    SendNotification(ctx context.Context, message domain.Outgoing) (domain.MessageReceipt, error)
}

type NotificationCases interface {
    Plan(ctx context.Context, now time.Time) error
    DispatchOne(ctx context.Context, now time.Time) (bool, error)
    ExpireDialogs(ctx context.Context, now time.Time) error
}
```

NotificationCases — потребляемый scheduler интерфейс. SendNotification делает
одну попытку HTTP; повторы и lease определяются сервисом/БД. Rate limiter
размещается в bot и общий для этого порта и интерактивных ответов.

Изменения доставки, отметки reminders.sent_at и состояния digest_run фиксируются
одной транзакцией после ответа Telegram. Обновление устаревшего lease отклоняется.
При старте и перед каждой доставкой данные/права заново проверяются.

## Ошибки и сообщения

Добавляются доменные ErrNotFound, ErrForbidden, ErrNotInGroup, ErrConflict,
ErrInvalidInput, ErrExpiredDialog. Существующие ошибки конфигурации,
пользователя, хранилища и Telegram сохраняются.

Русские сообщения об успехе и ошибках формируются в transport из типизированных
результатов. Базовые ответы: «Нет доступа к этому действию», «Сначала вступи
в группу», «Задание изменилось. Обнови карточку», «Диалог истёк. Начни заново»,
«Задание отменено», «Отмечено как выполненное», «Отметка снята».
Неизвестные SQL/API ошибки не показываются пользователю целиком.

## Уточнения при реализации

Экспортируемые методы и описанные поля — контракт этого этапа. Внутренние
неэкспортируемые функции, конструкторы, разбиение файлов, локальные SQL-хелперы
и детали fake-реализаций выбираются Executor без нового согласования.
При обнаружении недостающего внешнего метода/поля Lead фиксирует конкретное
изменение и согласует поправку контракта по правилам AGENTS.md.
