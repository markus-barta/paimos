package db

import (
	"database/sql"
	"testing"
)

// PAI-577: issues.content_rev must be bumped by triggers whenever a field the
// issue list renders from another table changes — booked/time (time_entries),
// the TAGS column (issue_tags assignment + tag rename), and sprint membership
// (issue_relations). The conditional-GET ETag folds in SUM(content_rev), so a
// missing bump means a stale list served via 304.

func lastID(t *testing.T, res sql.Result) int64 {
	t.Helper()
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
	return res
}

func contentRevOf(t *testing.T, db *sql.DB, issueID int64) int64 {
	t.Helper()
	var rev int64
	if err := db.QueryRow("SELECT content_rev FROM issues WHERE id=?", issueID).Scan(&rev); err != nil {
		t.Fatalf("read content_rev(%d): %v", issueID, err)
	}
	return rev
}

func TestContentRevBumpsOnListDerivedMutations(t *testing.T) {
	db := openTestDB(t)

	uid := lastID(t, mustExec(t, db, "INSERT INTO users(username,password,role,status) VALUES('worker','x','member','active')"))
	pid := lastID(t, mustExec(t, db, "INSERT INTO projects(name,key) VALUES('Proj','PRJ')"))
	iid := lastID(t, mustExec(t, db, "INSERT INTO issues(project_id,issue_number,title,type,status,priority) VALUES(?,1,'Issue','ticket','backlog','medium')", pid))
	sid := lastID(t, mustExec(t, db, "INSERT INTO issues(project_id,issue_number,title,type,status,priority) VALUES(?,2,'Sprint','sprint','backlog','medium')", pid))
	tagID := lastID(t, mustExec(t, db, "INSERT INTO tags(name,color,description) VALUES('bug','red','')"))

	if rev := contentRevOf(t, db, iid); rev != 0 {
		t.Fatalf("initial content_rev = %d, want 0", rev)
	}

	rev := int64(0)
	bump := func(label string, action func()) {
		action()
		got := contentRevOf(t, db, iid)
		if got <= rev {
			t.Fatalf("%s: content_rev did not increase (was %d, now %d)", label, rev, got)
		}
		rev = got
	}

	var teID int64
	bump("time_entry insert", func() {
		teID = lastID(t, mustExec(t, db, "INSERT INTO time_entries(issue_id,user_id,started_at,override) VALUES(?,?,?,?)", iid, uid, "2026-01-01T00:00:00Z", 2.0))
	})
	bump("time_entry update", func() {
		mustExec(t, db, "UPDATE time_entries SET override=3.0 WHERE id=?", teID)
	})
	bump("time_entry delete", func() {
		mustExec(t, db, "DELETE FROM time_entries WHERE id=?", teID)
	})

	bump("tag assign", func() {
		mustExec(t, db, "INSERT INTO issue_tags(issue_id,tag_id) VALUES(?,?)", iid, tagID)
	})
	bump("tag rename (chip text changes)", func() {
		mustExec(t, db, "UPDATE tags SET name='defect' WHERE id=?", tagID)
	})
	bump("tag unassign", func() {
		mustExec(t, db, "DELETE FROM issue_tags WHERE issue_id=? AND tag_id=?", iid, tagID)
	})

	bump("sprint membership add", func() {
		mustExec(t, db, "INSERT INTO issue_relations(source_id,target_id,type) VALUES(?,?,'sprint')", sid, iid)
	})
	bump("sprint membership remove", func() {
		mustExec(t, db, "DELETE FROM issue_relations WHERE source_id=? AND target_id=? AND type='sprint'", sid, iid)
	})
}
