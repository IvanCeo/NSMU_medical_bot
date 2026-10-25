package service

import (
	"context"
	"strings"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) createGroup(ctx context.Context, r Repositories, u domain.User, title string) (domain.Group, error) {
	var g domain.Group
	if e := requireAdmin(u); e != nil {
		return g, e
	}
	title = strings.TrimSpace(title)
	if !validText(title, 200) {
		return g, domain.ErrInvalidInput
	}
	code, e := s.token()
	if e != nil {
		return g, e
	}
	g, e = r.Groups.Create(ctx, domain.Group{Title: title, InviteCode: code, CreatedAt: s.now(), DigestEnabled: true})
	if e != nil {
		return g, e
	}
	return g, s.log(ctx, r, u, g.ID, "create", "group", g.ID)
}
func (s *App) CreateGroup(ctx context.Context, id int64, title string) (domain.Group, error) {
	var g domain.Group
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		g, e = s.createGroup(ctx, r, u, title)
		return e
	})
	return g, e
}
func (s *App) Groups(ctx context.Context, id int64, p domain.PageRequest) (domain.GroupPage, error) {
	var out domain.GroupPage
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := requireAdmin(u); e != nil {
			return e
		}
		var e error
		out, e = r.Groups.List(ctx, p)
		return e
	})
	return out, e
}
func (s *App) updateGroup(ctx context.Context, r Repositories, u domain.User, gid, v int64, p domain.GroupPatch) (domain.Group, error) {
	var g domain.Group
	if e := requireAdmin(u); e != nil {
		return g, e
	}
	g, e := r.Groups.ByID(ctx, gid)
	if e != nil {
		return g, e
	}
	if g.Version != v {
		return g, domain.ErrConflict
	}
	if p.Title != nil {
		g.Title = strings.TrimSpace(*p.Title)
		if !validText(g.Title, 200) {
			return g, domain.ErrInvalidInput
		}
	}
	if p.DigestEnabled != nil {
		g.DigestEnabled = *p.DigestEnabled
		if !g.DigestEnabled {
			if e := r.Deliveries.CancelForGroup(ctx, gid, s.now()); e != nil {
				return g, e
			}
		}
	}
	if e := r.Groups.Update(ctx, g, v); e != nil {
		return g, e
	}
	g.Version++
	return g, s.log(ctx, r, u, gid, "update", "group", gid)
}
func (s *App) UpdateGroup(ctx context.Context, id, gid, v int64, p domain.GroupPatch) (domain.Group, error) {
	var g domain.Group
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		g, e = s.updateGroup(ctx, r, u, gid, v, p)
		return e
	})
	return g, e
}
func (s *App) rotateInvite(ctx context.Context, r Repositories, u domain.User, gid int64) (string, error) {
	if e := requireAdmin(u); e != nil {
		return "", e
	}
	g, e := r.Groups.ByID(ctx, gid)
	if e != nil {
		return "", e
	}
	code, e := s.token()
	if e != nil {
		return "", e
	}
	g.InviteCode = code
	if e := r.Groups.Update(ctx, g, g.Version); e != nil {
		return "", e
	}
	return code, s.log(ctx, r, u, gid, "rotate_invite", "group", gid)
}
func (s *App) RotateInvite(ctx context.Context, id, gid int64) (string, error) {
	var code string
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		code, e = s.rotateInvite(ctx, r, u, gid)
		return e
	})
	return code, e
}
func (s *App) BindChat(ctx context.Context, id, gid, chat int64) error {
	if chat >= 0 {
		return domain.ErrInvalidInput
	}
	// Telegram permission checks happen outside the transaction and are followed by a fresh role check.
	if e := s.actor(ctx, id, func(r Repositories, u domain.User) error { return requireAdmin(u) }); e != nil {
		return e
	}
	p, e := s.verifier.CheckBinding(ctx, chat, id)
	if e != nil {
		return e
	}
	if !p.ActorIsAdministrator || !p.BotIsAdministrator || !p.BotCanSend {
		return domain.ErrForbidden
	}
	return s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := requireAdmin(u); e != nil {
			return e
		}
		g, e := r.Groups.ByID(ctx, gid)
		if e != nil {
			return e
		}
		if g.TelegramChatID != nil && *g.TelegramChatID == chat {
			return nil
		}
		if e := r.Deliveries.CancelForGroup(ctx, gid, s.now()); e != nil {
			return e
		}
		g.TelegramChatID = &chat
		g.ChatBindingVersion++
		if e := r.Groups.Update(ctx, g, g.Version); e != nil {
			return e
		}
		return s.log(ctx, r, u, gid, "bind", "group", gid)
	})
}
func (s *App) Members(ctx context.Context, id, gid int64, p domain.PageRequest) (domain.UserPage, error) {
	var out domain.UserPage
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := requireAdmin(u); e != nil {
			return e
		}
		var e error
		out, e = r.Users.ListByGroup(ctx, gid, p)
		return e
	})
	return out, e
}
func (s *App) setEditor(ctx context.Context, r Repositories, u domain.User, uid int64, editor bool) error {
	if e := requireAdmin(u); e != nil {
		return e
	}
	target, e := r.Users.ByID(ctx, uid)
	if e != nil {
		return e
	}
	if target.Role == domain.Admin || target.GroupID == nil {
		return domain.ErrForbidden
	}
	role := domain.Student
	if editor {
		role = domain.Editor
	}
	if e := r.Users.SetRole(ctx, uid, role); e != nil {
		return e
	}
	return s.log(ctx, r, u, *target.GroupID, "role", "user", uid)
}
func (s *App) SetEditor(ctx context.Context, id, uid int64, editor bool) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return s.setEditor(ctx, r, u, uid, editor) })
}
func (s *App) removeMember(ctx context.Context, r Repositories, u domain.User, uid int64) error {
	if e := requireAdmin(u); e != nil {
		return e
	}
	target, e := r.Users.ByID(ctx, uid)
	if e != nil {
		return e
	}
	if target.GroupID == nil {
		return domain.ErrNotInGroup
	}
	if target.Role != domain.Student {
		return domain.ErrForbidden
	}
	if e := s.cancelUser(ctx, r, uid, nil); e != nil {
		return e
	}
	if e := r.Users.SetGroup(ctx, uid, nil); e != nil {
		return e
	}
	return s.log(ctx, r, u, *target.GroupID, "remove", "user", uid)
}
func (s *App) RemoveMember(ctx context.Context, id, uid int64) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return s.removeMember(ctx, r, u, uid) })
}
