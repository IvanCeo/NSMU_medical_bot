package service

import (
	"context"
	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"time"
)

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

type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(Repositories) error) error
}

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
