package domain

import "time"

type Role string

const (
	Student Role = "student"
	Editor  Role = "editor"
	Admin   Role = "admin"
)

type User struct {
	ID, TelegramID       int64
	DisplayName          string
	GroupID              *int64
	Role                 Role
	CreatedAt            time.Time
	PrivateChatAvailable bool
}
type Group struct {
	ID                          int64
	Title, InviteCode           string
	CreatedAt                   time.Time
	TelegramChatID              *int64
	DigestEnabled               bool
	Version, ChatBindingVersion int64
}
type Profile struct {
	User        User
	Group       *Group
	Preferences Preferences
}
type QuietHours struct{ Start, End string }
type Preferences struct {
	RemindersEnabled bool
	QuietHours       *QuietHours
}
type Subject struct {
	ID, GroupID          int64
	Name, Teacher        string
	ArchivedAt           *time.Time
	Version              int64
	CreatedAt, UpdatedAt time.Time
}
type AssignmentKind string
type Importance string
type AssignmentStatus string

const (
	Homework  AssignmentKind   = "homework"
	Test      AssignmentKind   = "test"
	Exam      AssignmentKind   = "exam"
	Practice  AssignmentKind   = "practice"
	Other     AssignmentKind   = "other"
	Low       Importance       = "low"
	Normal    Importance       = "normal"
	High      Importance       = "high"
	Active    AssignmentStatus = "active"
	Cancelled AssignmentStatus = "cancelled"
)

type AttachmentKind string

const (
	Document AttachmentKind = "document"
	Photo    AttachmentKind = "photo"
)

type AttachmentInput struct {
	Kind                   AttachmentKind
	FileID, FileName, MIME string
}
type Attachment struct {
	ID, AssignmentID int64
	AttachmentInput
}
type Assignment struct {
	ID, GroupID, SubjectID int64
	Kind                   AssignmentKind
	Title, Description     string
	DeadlineAt             time.Time
	Importance             Importance
	URL                    string
	CreatedBy              int64
	Status                 AssignmentStatus
	Version                int64
	CreatedAt, UpdatedAt   time.Time
	Attachments            []Attachment
}
type AssignmentView struct {
	Assignment
	Done bool
}
type AssignmentInput struct {
	GroupID, SubjectID int64
	Kind               AssignmentKind
	Title, Description string
	DeadlineAt         time.Time
	Importance         Importance
	URL                string
	Attachments        []AttachmentInput
}
type AssignmentPatch struct {
	SubjectID          *int64
	Kind               *AssignmentKind
	Title, Description *string
	DeadlineAt         *time.Time
	Importance         *Importance
	URL                *string
	Attachments        *[]AttachmentInput
}
type SubjectInput struct {
	GroupID       int64
	Name, Teacher string
}
type SubjectPatch struct {
	Name, Teacher *string
	Archived      *bool
}
type GroupPatch struct {
	Title         *string
	DigestEnabled *bool
}
type PageRequest struct {
	Cursor string
	Limit  int
}
type GroupPage struct {
	Items      []Group
	NextCursor string
}
type UserPage struct {
	Items      []User
	NextCursor string
}
type SubjectPage struct {
	Items      []Subject
	NextCursor string
}
type AssignmentPage struct {
	Items      []AssignmentView
	NextCursor string
}
type AuditPage struct {
	Items      []AuditEntry
	NextCursor string
}
type AssignmentFilter struct {
	GroupID            int64
	SubjectID          *int64
	Period, DoneFilter string
	// Bounds are resolved by the service using its configured timezone.
	From, Until *time.Time
	Before      *time.Time
	After       *time.Time
}
type ReminderSource string

const (
	Standard ReminderSource = "default"
	Snoozed  ReminderSource = "snooze"
)

type ReminderPlan struct {
	AssignmentID, UserID, AssignmentVersion int64
	Source                                  ReminderSource
	Offset                                  *time.Duration
	SendAt                                  time.Time
}
type Reminder struct {
	ID int64
	ReminderPlan
	SentAt, CancelledAt *time.Time
}
type DialogKind string
type DialogStatus string

const (
	DialogJoin       DialogKind   = "join"
	DialogLeave      DialogKind   = "leave"
	DialogAdd        DialogKind   = "add"
	DialogEdit       DialogKind   = "edit"
	DialogCancelTask DialogKind   = "cancel_task"
	DialogAdmin      DialogKind   = "admin"
	DialogSettings   DialogKind   = "settings"
	DialogSubjects   DialogKind   = "subjects"
	DialogActive     DialogStatus = "active"
	DialogCompleted  DialogStatus = "completed"
	DialogCancelled  DialogStatus = "cancelled"
	DialogExpired    DialogStatus = "expired"
)

type Dialog struct {
	ID, UserID           int64
	Kind                 DialogKind
	Step, Payload        string
	Revision             int64
	Status               DialogStatus
	ExpiresAt, UpdatedAt time.Time
}
type DialogInput struct {
	Text, Selection *string
	Attachment      *AttachmentInput
}
type Option struct{ Label, Value string }
type DialogView struct {
	ID, Revision    int64
	Step, PromptKey string
	Options         []Option
	Preview         string
}
type Digest struct {
	Group       Group
	Date        time.Time
	Assignments []Assignment
}
type DigestStatus string

const (
	DigestPlanning  DigestStatus = "planning"
	DigestQueued    DigestStatus = "queued"
	DigestSent      DigestStatus = "sent"
	DigestEmpty     DigestStatus = "empty"
	DigestPartial   DigestStatus = "partial"
	DigestFailed    DigestStatus = "failed"
	DigestCancelled DigestStatus = "cancelled"
)

type DigestRun struct {
	ID, GroupID, BindingVersion int64
	LocalDate                   string
	Status                      DigestStatus
	CreatedAt                   time.Time
}
type DeliveryKind string
type DeliveryStatus string

const (
	ReminderDelivery          DeliveryKind   = "reminder"
	DigestDelivery            DeliveryKind   = "digest"
	AssignmentCreatedDelivery DeliveryKind   = "assignment_created"
	Pending                   DeliveryStatus = "pending"
	Processing                DeliveryStatus = "processing"
	Sent                      DeliveryStatus = "sent"
	Skipped                   DeliveryStatus = "skipped"
	Failed                    DeliveryStatus = "failed"
	DeliveryCancelled         DeliveryStatus = "cancelled"
)

type DeliveryPayload struct {
	AssignmentIDs           []int64
	Version, BindingVersion int64
	LocalDate               string
}
type DeliveryDraft struct {
	EventKey                 string
	Kind                     DeliveryKind
	ReminderID, DigestRunID  *int64
	AssignmentID             *int64
	GroupID                  int64
	UserID                   *int64
	Part                     int
	Payload                  DeliveryPayload
	NextAttemptAt, ExpiresAt time.Time
}
type Delivery struct {
	ID int64
	DeliveryDraft
	Status             DeliveryStatus
	Attempts           int
	LeaseToken         string
	LeaseUntil, SentAt *time.Time
	TelegramMessageID  *int
	LastErrorCode      string
}
type DeliveryFailure struct {
	Code       string
	Permanent  bool
	RetryAfter time.Duration
}

func (e *DeliveryFailure) Error() string { return "delivery failed: " + e.Code }

type DeliveryProgress struct{ Total, Pending, Sent, Skipped, Failed, Cancelled int }
type ChatPermissions struct{ ActorIsAdministrator, BotIsAdministrator, BotCanSend bool }
type OutgoingButton struct {
	Label        string
	AssignmentID int64
}
type Outgoing struct {
	ChatID     int64
	Kind, Text string
	EventKind  DeliveryKind
	Buttons    []OutgoingButton
}
type MessageReceipt struct {
	ChatID    int64
	MessageID int
}
type AuditEntry struct {
	ID               int64
	GroupID, ActorID *int64
	Action, Entity   string
	EntityID         int64
	Payload          string
	At               time.Time
}
