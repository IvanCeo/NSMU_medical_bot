package sqlite

import (
	"context"
	"database/sql"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

const userColumns = "id,telegram_id,display_name,group_id,role,created_at,private_chat_available"

func scanUser(row scanner) (domain.User, error) {
	var u domain.User
	var gid sql.NullInt64
	var at string
	err := row.Scan(&u.ID, &u.TelegramID, &u.DisplayName, &gid, &u.Role, &at, &u.PrivateChatAvailable)
	if gid.Valid {
		u.GroupID = &gid.Int64
	}
	u.CreatedAt = parse(at)
	return u, repoError(err)
}
func (r *Users) ByTelegramID(ctx context.Context, id int64) (domain.User, error) {
	return scanUser(r.q.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE telegram_id=?", id))
}
func (r *Users) ByID(ctx context.Context, id int64) (domain.User, error) {
	return scanUser(r.q.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", id))
}
func (r *Users) ListByGroup(ctx context.Context, gid int64, p domain.PageRequest) (out domain.UserPage, err error) {
	n, id, e := page(p)
	if e != nil {
		return out, e
	}
	rows, e := r.q.QueryContext(ctx, "SELECT "+userColumns+" FROM users WHERE group_id=? AND id>? ORDER BY id LIMIT ?", gid, id, n+1)
	if e != nil {
		return out, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, u)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = cursor(out.Items[n-1].ID)
	}
	return out, repoError(rows.Err())
}
func (r *Users) SetGroup(ctx context.Context, id int64, gid *int64) error {
	return cas(ctx, r.q, "UPDATE users SET group_id=? WHERE id=?", gid, id)
}
func (r *Users) SetRole(ctx context.Context, id int64, role domain.Role) error {
	return cas(ctx, r.q, "UPDATE users SET role=? WHERE id=?", role, id)
}
func (r *Users) SetPrivateChatAvailable(ctx context.Context, id int64, v bool) error {
	return cas(ctx, r.q, "UPDATE users SET private_chat_available=? WHERE id=?", v, id)
}

type groups struct{ store }

const groupColumns = "id,title,invite_code,created_at,telegram_chat_id,digest_enabled,version,chat_binding_version"

func scanGroup(row scanner) (domain.Group, error) {
	var g domain.Group
	var chat sql.NullInt64
	var at string
	e := row.Scan(&g.ID, &g.Title, &g.InviteCode, &at, &chat, &g.DigestEnabled, &g.Version, &g.ChatBindingVersion)
	if chat.Valid {
		g.TelegramChatID = &chat.Int64
	}
	g.CreatedAt = parse(at)
	return g, repoError(e)
}
func (r groups) Create(ctx context.Context, g domain.Group) (domain.Group, error) {
	res, e := r.q.ExecContext(ctx, "INSERT INTO groups(title,invite_code,created_at,digest_enabled,version,chat_binding_version) VALUES(?,?,?,?,1,0)", g.Title, g.InviteCode, stamp(g.CreatedAt), g.DigestEnabled)
	if e != nil {
		return g, repoError(e)
	}
	g.ID, e = res.LastInsertId()
	g.Version = 1
	return g, repoError(e)
}
func (r groups) ByID(ctx context.Context, id int64) (domain.Group, error) {
	return scanGroup(r.q.QueryRowContext(ctx, "SELECT "+groupColumns+" FROM groups WHERE id=?", id))
}
func (r groups) ByInviteCode(ctx context.Context, c string) (domain.Group, error) {
	return scanGroup(r.q.QueryRowContext(ctx, "SELECT "+groupColumns+" FROM groups WHERE invite_code=?", c))
}
func (r groups) Update(ctx context.Context, g domain.Group, v int64) error {
	return cas(ctx, r.q, "UPDATE groups SET title=?,invite_code=?,telegram_chat_id=?,digest_enabled=?,version=version+1,chat_binding_version=? WHERE id=? AND version=?", g.Title, g.InviteCode, g.TelegramChatID, g.DigestEnabled, g.ChatBindingVersion, g.ID, v)
}
func (r groups) List(ctx context.Context, p domain.PageRequest) (out domain.GroupPage, err error) {
	n, id, e := page(p)
	if e != nil {
		return out, e
	}
	rows, e := r.q.QueryContext(ctx, "SELECT "+groupColumns+" FROM groups WHERE id>? ORDER BY id LIMIT ?", id, n+1)
	if e != nil {
		return out, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		g, e := scanGroup(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, g)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = cursor(out.Items[n-1].ID)
	}
	return out, repoError(rows.Err())
}
