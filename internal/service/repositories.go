package service

type Repositories struct {
	Users       UserRepo
	Groups      GroupRepo
	Subjects    SubjectRepo
	Assignments AssignmentRepo
	Completions CompletionRepo
	Reminders   ReminderRepo
	Settings    SettingsRepo
	Dialogs     DialogRepo
	Digests     DigestRepo
	Deliveries  DeliveryRepo
	Audit       AuditRepo
}
