package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type subjects struct{ store }

const subjectColumns = "id,group_id,name,COALESCE(teacher,''),archived_at,version,created_at,updated_at"

func scanSubject(row scanner) (domain.Subject, error) {
	var s domain.Subject
	var archived sql.NullString
	var created, updated string
	e := row.Scan(&s.ID, &s.GroupID, &s.Name, &s.Teacher, &archived, &s.Version, &created, &updated)
	s.ArchivedAt = nullableTime(archived)
	s.CreatedAt = parse(created)
	s.UpdatedAt = parse(updated)
	return s, repoError(e)
}
func (r subjects) Create(ctx context.Context, s domain.Subject) (domain.Subject, error) {
	res, e := r.q.ExecContext(ctx, "INSERT INTO subjects(group_id,name,teacher,version,created_at,updated_at) VALUES(?,?,?,1,?,?)", s.GroupID, s.Name, textArg(s.Teacher), stamp(s.CreatedAt), stamp(s.UpdatedAt))
	if e != nil {
		return s, repoError(e)
	}
	s.ID, e = res.LastInsertId()
	s.Version = 1
	return s, repoError(e)
}
func (r subjects) ByID(ctx context.Context, id int64) (domain.Subject, error) {
	return scanSubject(r.q.QueryRowContext(ctx, "SELECT "+subjectColumns+" FROM subjects WHERE id=?", id))
}
func (r subjects) List(ctx context.Context, gid int64, archived bool, p domain.PageRequest) (out domain.SubjectPage, err error) {
	n, id, e := page(p)
	if e != nil {
		return out, e
	}
	rows, e := r.q.QueryContext(ctx, "SELECT "+subjectColumns+" FROM subjects WHERE group_id=? AND (? OR archived_at IS NULL) AND id>? ORDER BY id LIMIT ?", gid, archived, id, n+1)
	if e != nil {
		return out, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		s, e := scanSubject(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, s)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = cursor(out.Items[n-1].ID)
	}
	return out, repoError(rows.Err())
}
func (r subjects) Update(ctx context.Context, s domain.Subject, v int64) error {
	return cas(ctx, r.q, "UPDATE subjects SET name=?,teacher=?,archived_at=?,version=version+1,updated_at=? WHERE id=? AND version=?", s.Name, textArg(s.Teacher), timeArg(s.ArchivedAt), stamp(s.UpdatedAt), s.ID, v)
}

type assignments struct{ store }

const assignmentColumns = "a.id,a.group_id,a.subject_id,a.kind,a.title,a.description,a.deadline_at,a.importance,COALESCE(a.url,''),a.created_by,a.status,a.version,a.created_at,a.updated_at"

func scanAssignment(row scanner) (domain.Assignment, error) {
	var a domain.Assignment
	var deadline, created, updated string
	e := row.Scan(&a.ID, &a.GroupID, &a.SubjectID, &a.Kind, &a.Title, &a.Description, &deadline, &a.Importance, &a.URL, &a.CreatedBy, &a.Status, &a.Version, &created, &updated)
	a.DeadlineAt = parse(deadline)
	a.CreatedAt = parse(created)
	a.UpdatedAt = parse(updated)
	return a, repoError(e)
}
func (r assignments) Create(ctx context.Context, a domain.Assignment) (domain.Assignment, error) {
	res, e := r.q.ExecContext(ctx, `INSERT INTO assignments(group_id,subject_id,kind,title,description,deadline_at,importance,url,created_by,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'active',1,?,?)`, a.GroupID, a.SubjectID, a.Kind, a.Title, a.Description, stamp(a.DeadlineAt), a.Importance, textArg(a.URL), a.CreatedBy, stamp(a.CreatedAt), stamp(a.UpdatedAt))
	if e != nil {
		return a, repoError(e)
	}
	a.ID, e = res.LastInsertId()
	if e != nil {
		return a, repoError(e)
	}
	a.Version = 1
	a.Status = domain.Active
	return a, r.saveAttachments(ctx, a)
}
func (r assignments) saveAttachments(ctx context.Context, a domain.Assignment) error {
	previous, e := r.loadAttachments(ctx, a.ID)
	if e != nil {
		return e
	}
	same := len(previous) == len(a.Attachments)
	if same {
		for i, f := range previous {
			if f.AttachmentInput != a.Attachments[i].AttachmentInput {
				same = false
				break
			}
		}
	}
	if same {
		return nil
	}
	if e := exec(ctx, r.q, "DELETE FROM attachments WHERE assignment_id=?", a.ID); e != nil {
		return e
	}
	for _, f := range a.Attachments {
		if e := exec(ctx, r.q, "INSERT INTO attachments(assignment_id,kind,file_id,file_name,mime) VALUES(?,?,?,?,?)", a.ID, f.Kind, f.FileID, f.FileName, f.MIME); e != nil {
			return e
		}
	}
	return nil
}
func (r assignments) ByID(ctx context.Context, id int64) (a domain.Assignment, err error) {
	a, e := scanAssignment(r.q.QueryRowContext(ctx, "SELECT "+assignmentColumns+" FROM assignments a WHERE a.id=?", id))
	if e != nil {
		return a, e
	}
	a.Attachments, e = r.loadAttachments(ctx, id)
	return a, e
}
func (r assignments) loadAttachments(ctx context.Context, id int64) (out []domain.Attachment, err error) {
	rows, e := r.q.QueryContext(ctx, "SELECT id,assignment_id,kind,file_id,file_name,mime FROM attachments WHERE assignment_id=? ORDER BY id", id)
	if e != nil {
		return out, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		var f domain.Attachment
		if e := rows.Scan(&f.ID, &f.AssignmentID, &f.Kind, &f.FileID, &f.FileName, &f.MIME); e != nil {
			return out, repoError(e)
		}
		out = append(out, f)
	}
	return out, repoError(rows.Err())
}
func (r assignments) Update(ctx context.Context, a domain.Assignment, v int64) error {
	e := cas(ctx, r.q, `UPDATE assignments SET subject_id=?,kind=?,title=?,description=?,deadline_at=?,importance=?,url=?,status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, a.SubjectID, a.Kind, a.Title, a.Description, stamp(a.DeadlineAt), a.Importance, textArg(a.URL), a.Status, stamp(a.UpdatedAt), a.ID, v)
	if e != nil {
		return e
	}
	return r.saveAttachments(ctx, a)
}
func (r assignments) List(ctx context.Context, f domain.AssignmentFilter, uid int64, p domain.PageRequest) (domain.AssignmentPage, error) {
	var out domain.AssignmentPage
	n, t, id, e := assignmentPage(p)
	if e != nil {
		return out, e
	}
	query := "SELECT " + assignmentColumns + ` FROM assignments a WHERE a.group_id=? AND a.status='active' AND (? IS NULL OR a.subject_id=?) AND (? IS NULL OR a.deadline_at>=?) AND (? IS NULL OR a.deadline_at<?) AND (? IS NULL OR a.deadline_at<?) AND (? IS NULL OR a.deadline_at>?) AND (a.deadline_at>? OR (a.deadline_at=? AND a.id>?)) AND (?='all' OR (?='done')=EXISTS(SELECT 1 FROM completions c WHERE c.assignment_id=a.id AND c.user_id=?)) ORDER BY a.deadline_at,a.id LIMIT ?`
	done := f.DoneFilter
	if done == "" {
		done = "all"
	}
	rows, e := r.q.QueryContext(ctx, query, f.GroupID, f.SubjectID, f.SubjectID, timeArg(f.From), timeArg(f.From), timeArg(f.Until), timeArg(f.Until), timeArg(f.Before), timeArg(f.Before), timeArg(f.After), timeArg(f.After), t, t, id, done, done, uid, n+1)
	if e != nil {
		return out, repoError(e)
	}
	out.Items, e = readAssignments(rows)
	if e != nil {
		return out, repoError(e)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		last := out.Items[n-1]
		out.NextCursor = assignmentCursor(last.DeadlineAt, last.ID)
	}
	for i := range out.Items {
		out.Items[i].Done, e = completions(r).IsDone(ctx, uid, out.Items[i].ID)
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

type completions struct{ store }

func (r completions) IsDone(ctx context.Context, uid, aid int64) (bool, error) {
	var b bool
	e := r.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM completions WHERE user_id=? AND assignment_id=?)", uid, aid).Scan(&b)
	return b, repoError(e)
}
func (r completions) Set(ctx context.Context, uid, aid int64, at *time.Time) error {
	if at == nil {
		return exec(ctx, r.q, "DELETE FROM completions WHERE user_id=? AND assignment_id=?", uid, aid)
	}
	return exec(ctx, r.q, "INSERT INTO completions(user_id,assignment_id,done_at) VALUES(?,?,?) ON CONFLICT(user_id,assignment_id) DO NOTHING", uid, aid, stamp(*at))
}
