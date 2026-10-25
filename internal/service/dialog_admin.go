package service

import (
	"context"
	"strings"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) advanceAdmin(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text, choice string) error {
	if e := requireAdmin(u); e != nil {
		return e
	}
	switch d.Step {
	case "admin_groups":
		return s.advanceAdminGroups(ctx, r, d, p, choice)
	case "group_title":
		return s.advanceAdminGroupTitle(ctx, r, u, d, p, text)
	case "admin_group":
		return s.advanceAdminGroup(ctx, r, u, d, p, choice)
	case "member_list":
		return s.advanceAdminMemberList(ctx, r, d, p, choice)
	case "member_action":
		return s.advanceAdminMemberAction(ctx, r, u, d, p, choice)
	case "member_remove_confirm":
		return s.advanceAdminMemberRemoveConfirm(ctx, r, u, d, p, choice)
	case "admin_digest":
		if choice != "back" {
			return domain.ErrInvalidInput
		}
		d.Step = "admin_group"
	case "admin_audit":
		return s.advanceAdminAudit(ctx, r, d, p, choice)
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceSubject(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text, choice string) error {
	if e := access(u, p.GroupID, true); e != nil {
		return e
	}
	switch d.Step {
	case "subject_menu":
		return s.advanceSubjectMenu(d, p, choice)
	case "subject_list":
		return s.advanceSubjectList(ctx, r, d, p, choice)
	case "subject_action":
		return s.advanceSubjectAction(ctx, r, u, d, p, choice)
	case "subject_name":
		return s.advanceSubjectName(ctx, r, u, d, p, text)
	case "subject_teacher":
		return s.advanceSubjectTeacher(ctx, r, u, d, p, text)
	default:
		return domain.ErrInvalidInput
	}
}

func (s *App) advanceAdminGroups(ctx context.Context, r Repositories, d *domain.Dialog, p *dialogDraft, choice string) error {
	switch choice {
	case "create":
		d.Step = "group_title"
		p.Action = "create"
	case "next":
		gs, e := r.Groups.List(ctx, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
		if e != nil {
			return e
		}
		p.Cursor = gs.NextCursor
	default:
		gid, e := parseID(choice)
		if e != nil {
			return e
		}
		g, e := r.Groups.ByID(ctx, gid)
		if e != nil {
			return e
		}
		p.GroupID = gid
		p.Version = g.Version
		p.Cursor = ""
		d.Step = "admin_group"
	}
	return nil
}

func (s *App) advanceAdminGroupTitle(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text string) error {
	if p.Action == "create" {
		g, e := s.createGroup(ctx, r, u, text)
		if e != nil {
			return e
		}
		p.GroupID = g.ID
		p.Version = g.Version
	} else {
		_, e := s.updateGroup(ctx, r, u, p.GroupID, p.Version, domain.GroupPatch{Title: &text})
		if e != nil {
			return e
		}
	}
	d.Step = "admin_group"
	return nil
}

func (s *App) advanceAdminGroup(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	g, e := r.Groups.ByID(ctx, p.GroupID)
	if e != nil {
		return e
	}
	p.Version = g.Version
	switch choice {
	case "rename":
		p.Action = "rename"
		d.Step = "group_title"
	case "rotate":
		if _, e := s.rotateInvite(ctx, r, u, g.ID); e != nil {
			return e
		}
	case "digest_toggle":
		enabled := !g.DigestEnabled
		if _, e := s.updateGroup(ctx, r, u, g.ID, g.Version, domain.GroupPatch{DigestEnabled: &enabled}); e != nil {
			return e
		}
	case "members":
		p.Cursor = ""
		d.Step = "member_list"
	case "subjects":
		d.Step = "subject_menu"
	case "add":
		d.Step = "task_subject"
	case "digest_preview":
		d.Step = "admin_digest"
	case "audit":
		p.Cursor = ""
		d.Step = "admin_audit"
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceAdminMemberList(ctx context.Context, r Repositories, d *domain.Dialog, p *dialogDraft, choice string) error {
	if choice == "next" {
		list, e := r.Users.ListByGroup(ctx, p.GroupID, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
		if e != nil {
			return e
		}
		p.Cursor = list.NextCursor
		return nil
	}
	id, e := parseID(choice)
	if e != nil {
		return e
	}
	target, e := r.Users.ByID(ctx, id)
	if e != nil {
		return e
	}
	if target.GroupID == nil || *target.GroupID != p.GroupID {
		return domain.ErrForbidden
	}
	p.TargetUserID = id
	d.Step = "member_action"
	return nil
}

func (s *App) advanceAdminAudit(ctx context.Context, r Repositories, d *domain.Dialog, p *dialogDraft, choice string) error {
	switch choice {
	case "next":
		a, e := r.Audit.List(ctx, p.GroupID, domain.PageRequest{Cursor: p.Cursor, Limit: 10})
		if e != nil {
			return e
		}
		p.Cursor = a.NextCursor
	case "back":
		p.Cursor = ""
		d.Step = "admin_group"
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceSubjectList(ctx context.Context, r Repositories, d *domain.Dialog, p *dialogDraft, choice string) error {
	if choice == "next" {
		list, e := r.Subjects.List(ctx, p.GroupID, true, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
		if e != nil {
			return e
		}
		p.Cursor = list.NextCursor
		return nil
	}
	id, e := parseID(choice)
	if e != nil {
		return e
	}
	sub, e := r.Subjects.ByID(ctx, id)
	if e != nil {
		return e
	}
	if sub.GroupID != p.GroupID {
		return domain.ErrForbidden
	}
	p.EntityID = id
	p.SubjectVersion = sub.Version
	p.Subject = domain.SubjectInput{GroupID: sub.GroupID, Name: sub.Name, Teacher: sub.Teacher}
	d.Step = "subject_action"
	return nil
}

func (s *App) advanceSubjectAction(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	p.Action = choice
	switch choice {
	case "rename_subject":
		d.Step = "subject_name"
	case "teacher":
		d.Step = "subject_teacher"
	case "archive", "restore":
		archived := choice == "archive"
		if _, e := s.updateSubject(ctx, r, u, p.EntityID, p.SubjectVersion, domain.SubjectPatch{Archived: &archived}); e != nil {
			return e
		}
		d.Step = "subject_menu"
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceSubjectName(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text string) error {
	text = strings.TrimSpace(text)
	if !validText(text, 200) {
		return domain.ErrInvalidInput
	}
	p.Subject.Name = text
	if p.Action == "create" {
		d.Step = "subject_teacher"
	} else {
		if _, e := s.updateSubject(ctx, r, u, p.EntityID, p.SubjectVersion, domain.SubjectPatch{Name: &text}); e != nil {
			return e
		}
		d.Step = "subject_menu"
	}
	return nil
}

func (s *App) advanceSubjectTeacher(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text string) error {
	text = strings.TrimSpace(text)
	if text == "-" {
		text = ""
	}
	p.Subject.Teacher = text
	if p.Action == "create" {
		if _, e := s.createSubject(ctx, r, u, p.Subject); e != nil {
			return e
		}
	} else {
		if _, e := s.updateSubject(ctx, r, u, p.EntityID, p.SubjectVersion, domain.SubjectPatch{Teacher: &text}); e != nil {
			return e
		}
	}
	d.Step = "subject_menu"
	return nil
}

func (s *App) advanceAdminMemberAction(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	switch choice {
	case "grant_editor", "revoke_editor":
		if e := s.setEditor(ctx, r, u, p.TargetUserID, choice == "grant_editor"); e != nil {
			return e
		}
		d.Step = "admin_group"
	case "remove_member":
		d.Step = "member_remove_confirm"
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceAdminMemberRemoveConfirm(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	if choice != "confirm" {
		return domain.ErrInvalidInput
	}
	if e := s.removeMember(ctx, r, u, p.TargetUserID); e != nil {
		return e
	}
	d.Step = "admin_group"
	return nil
}

func (s *App) advanceSubjectMenu(d *domain.Dialog, p *dialogDraft, choice string) error {
	switch choice {
	case "create_subject":
		p.Action = "create"
		p.Subject = domain.SubjectInput{GroupID: p.GroupID}
		d.Step = "subject_name"
	case "edit_subject":
		p.Cursor = ""
		d.Step = "subject_list"
	default:
		return domain.ErrInvalidInput
	}
	return nil
}
