package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Options struct {
	Now             func() time.Time
	Token           func() (string, error)
	Timezone        *time.Location
	ReminderOffsets []time.Duration
	DigestTime      string
}
type App struct {
	work       UnitOfWork
	now        func() time.Time
	token      func() (string, error)
	tz         *time.Location
	offsets    []time.Duration
	digestTime string
	verifier   ChatVerifier
	sender     NotificationSender
}

func New(work UnitOfWork, verifier ChatVerifier, sender NotificationSender, o Options) *App {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Token == nil {
		o.Token = randomToken
	}
	if o.Timezone == nil {
		o.Timezone = time.UTC
	}
	if o.DigestTime == "" {
		o.DigestTime = "08:00"
	}
	return &App{work: work, now: o.Now, token: o.Token, tz: o.Timezone, offsets: append([]time.Duration(nil), o.ReminderOffsets...), digestTime: o.DigestTime, verifier: verifier, sender: sender}
}
func randomToken() (string, error) {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", fmt.Errorf("generate token: %w", e)
	}
	return hex.EncodeToString(b[:]), nil
}
func (s *App) actor(ctx context.Context, id int64, fn func(Repositories, domain.User) error) error {
	if id <= 0 {
		return domain.ErrInvalidUser
	}
	return s.work.WithinTx(ctx, func(r Repositories) error {
		u, e := r.Users.ByTelegramID(ctx, id)
		if e != nil {
			return e
		}
		return fn(r, u)
	})
}
func requireAdmin(u domain.User) error {
	if u.Role != domain.Admin {
		return domain.ErrForbidden
	}
	return nil
}
func access(u domain.User, gid int64, write bool) error {
	if gid <= 0 {
		return domain.ErrNotInGroup
	}
	if u.Role == domain.Admin {
		return nil
	}
	if u.GroupID == nil {
		return domain.ErrNotInGroup
	}
	if *u.GroupID != gid {
		return domain.ErrForbidden
	}
	if write && u.Role != domain.Editor {
		return domain.ErrForbidden
	}
	return nil
}
func groupID(u domain.User, explicit int64) (int64, error) {
	if explicit > 0 {
		return explicit, access(u, explicit, false)
	}
	if u.GroupID == nil {
		return 0, domain.ErrNotInGroup
	}
	return *u.GroupID, nil
}
func validText(v string, max int) bool {
	return strings.TrimSpace(v) != "" && utf8.RuneCountInString(v) <= max
}
func (s *App) log(ctx context.Context, r Repositories, u domain.User, gid int64, action, entity string, id int64) error {
	var group *int64
	if gid > 0 {
		group = &gid
	}
	fields := []string{"group_id"}
	switch entity {
	case "group":
		fields = []string{"title", "invite_code", "telegram_chat_id", "digest_enabled"}
	case "subject":
		fields = []string{"name", "teacher", "archived_at"}
	case "assignment":
		fields = []string{"subject_id", "kind", "title", "description", "deadline_at", "importance", "url", "attachments", "status"}
	}
	if action == "role" {
		fields = []string{"role"}
	}
	if action == "settings" {
		fields = []string{"reminders_enabled", "quiet_start", "quiet_end"}
	}
	if action == "completion" {
		fields = []string{"done_at"}
	}
	payload, e := json.Marshal(struct {
		Fields []string `json:"fields"`
	}{fields})
	if e != nil {
		return fmt.Errorf("encode audit: %w", e)
	}
	return r.Audit.Append(ctx, domain.AuditEntry{GroupID: group, ActorID: &u.ID, Action: action, Entity: entity, EntityID: id, Payload: string(payload), At: s.now().UTC()})
}
func (s *App) cancelUser(ctx context.Context, r Repositories, uid int64, aid *int64) error {
	if e := r.Reminders.CancelForUser(ctx, uid, aid, s.now()); e != nil {
		return e
	}
	return r.Deliveries.CancelForUser(ctx, uid, aid, s.now())
}
func (s *App) Register(ctx context.Context, t domain.TelegramUser) (domain.User, error) {
	var u domain.User
	if t.TelegramID <= 0 || utf8.RuneCountInString(t.DisplayName) > 256 {
		return u, domain.ErrInvalidUser
	}
	e := s.work.WithinTx(ctx, func(r Repositories) error {
		if e := r.Users.UpsertTelegramUser(ctx, t, s.now()); e != nil {
			return e
		}
		var e error
		u, e = r.Users.ByTelegramID(ctx, t.TelegramID)
		if e != nil {
			return e
		}
		wasAvailable := u.PrivateChatAvailable
		u.PrivateChatAvailable = true
		if e := r.Users.SetPrivateChatAvailable(ctx, u.ID, true); e != nil {
			return e
		}
		if !wasAvailable && u.GroupID != nil {
			return s.scheduleUser(ctx, r, u)
		}
		return nil
	})
	return u, e
}
func (s *App) profile(ctx context.Context, r Repositories, u domain.User) (domain.Profile, error) {
	p := domain.Profile{User: u}
	var e error
	p.Preferences, e = r.Settings.Get(ctx, u.ID)
	if e != nil {
		return p, e
	}
	if u.GroupID != nil {
		g, e := r.Groups.ByID(ctx, *u.GroupID)
		if e != nil {
			return p, e
		}
		if u.Role == domain.Student {
			g.InviteCode = ""
		}
		p.Group = &g
	}
	return p, nil
}
func (s *App) Profile(ctx context.Context, id int64) (domain.Profile, error) {
	var p domain.Profile
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error { var e error; p, e = s.profile(ctx, r, u); return e })
	return p, e
}
func (s *App) join(ctx context.Context, r Repositories, u domain.User, code string) (domain.Profile, error) {
	if len(code) > 64 || code == "" {
		return domain.Profile{}, domain.ErrInvalidInput
	}
	g, e := r.Groups.ByInviteCode(ctx, code)
	if e != nil {
		return domain.Profile{}, e
	}
	if u.GroupID != nil && *u.GroupID != g.ID {
		return domain.Profile{}, domain.ErrConflict
	}
	if u.GroupID == nil {
		if u.Role == domain.Editor {
			return domain.Profile{}, domain.ErrForbidden
		}
		if e := r.Users.SetGroup(ctx, u.ID, &g.ID); e != nil {
			return domain.Profile{}, e
		}
		u.GroupID = &g.ID
		if e := s.scheduleUser(ctx, r, u); e != nil {
			return domain.Profile{}, e
		}
		if e := s.log(ctx, r, u, g.ID, "join", "user", u.ID); e != nil {
			return domain.Profile{}, e
		}
	}
	return s.profile(ctx, r, u)
}
func (s *App) JoinGroup(ctx context.Context, id int64, code string) (domain.Profile, error) {
	var p domain.Profile
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		p, e = s.join(ctx, r, u, strings.TrimSpace(code))
		return e
	})
	return p, e
}
func (s *App) leave(ctx context.Context, r Repositories, u domain.User) error {
	if u.Role == domain.Editor {
		return domain.ErrForbidden
	}
	if u.GroupID == nil {
		return domain.ErrNotInGroup
	}
	gid := *u.GroupID
	if e := s.cancelUser(ctx, r, u.ID, nil); e != nil {
		return e
	}
	if e := r.Users.SetGroup(ctx, u.ID, nil); e != nil {
		return e
	}
	return s.log(ctx, r, u, gid, "leave", "user", u.ID)
}
func (s *App) LeaveGroup(ctx context.Context, id int64) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return s.leave(ctx, r, u) })
}
func (s *App) Preferences(ctx context.Context, id int64) (domain.Preferences, error) {
	var p domain.Preferences
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error { var e error; p, e = r.Settings.Get(ctx, u.ID); return e })
	return p, e
}
func validClock(v string) bool {
	t, e := time.Parse("15:04", v)
	return e == nil && t.Format("15:04") == v
}
func (s *App) savePreferences(ctx context.Context, r Repositories, u domain.User, p domain.Preferences) error {
	if q := p.QuietHours; q != nil && (!validClock(q.Start) || !validClock(q.End) || q.Start == q.End) {
		return domain.ErrInvalidInput
	}
	if e := r.Settings.Save(ctx, u.ID, p); e != nil {
		return e
	}
	if e := s.cancelUser(ctx, r, u.ID, nil); e != nil {
		return e
	}
	if p.RemindersEnabled && u.GroupID != nil {
		if e := s.scheduleUser(ctx, r, u); e != nil {
			return e
		}
	}
	gid := int64(0)
	if u.GroupID != nil {
		gid = *u.GroupID
	}
	return s.log(ctx, r, u, gid, "settings", "user", u.ID)
}
func (s *App) SavePreferences(ctx context.Context, id int64, p domain.Preferences) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return s.savePreferences(ctx, r, u, p) })
}
