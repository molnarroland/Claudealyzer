package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite" // pure Go, no CGO
)

// db reads opencode.db read-only. It supports the first table layout (session, message, part)
// and the later one (session_v2, session_message, session_part).
type db struct {
	conn *sql.DB
	v2   bool
}

func openDB(path string) (*db, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(3000)")
	q.Add("_pragma", "query_only(1)")
	u.RawQuery = q.Encode()
	conn, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	d := &db{conn: conn}
	tables := map[string]bool{}
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		conn.Close()
		return nil, err
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			tables[n] = true
		}
	}
	rows.Close()
	switch {
	case tables["session_v2"] && tables["session_message"] && tables["session_part"]:
		d.v2 = true
	case tables["session"] && tables["message"] && tables["part"]:
	default:
		conn.Close()
		return nil, fmt.Errorf("opencode.db has no session tables this version can read")
	}
	return d, nil
}

func (d *db) close() { _ = d.conn.Close() }

func (d *db) sessions() ([]session, error) {
	q := `SELECT s.id, COALESCE(s.parent_id, ''), COALESCE(s.directory, ''), COALESCE(s.title, ''), COALESCE(s.version, ''),
	             s.time_created, s.time_updated, (SELECT COUNT(*) FROM message m WHERE m.session_id = s.id)
	      FROM session s`
	if d.v2 {
		q = `SELECT s.id, COALESCE(s.parent_id, ''), COALESCE(s.directory, ''), COALESCE(s.title, ''), COALESCE(s.version, ''),
		            s.time_created, s.time_updated, (SELECT COUNT(*) FROM session_message m WHERE m.session_id = s.id)
		     FROM session_v2 s`
	}
	rows, err := d.conn.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session
	for rows.Next() {
		var s session
		if err := rows.Scan(&s.ID, &s.ParentID, &s.Dir, &s.Title, &s.Version, &s.Created, &s.Updated, &s.Messages); err != nil {
			return nil, err
		}
		if s.Messages > 0 {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

func (d *db) messages(sessionID string) ([]message, error) {
	mq := `SELECT id, '', time_created, data FROM message WHERE session_id = ? ORDER BY time_created, id`
	pq := `SELECT id, message_id, '', time_created, data FROM part WHERE session_id = ? ORDER BY message_id, id`
	if d.v2 {
		mq = `SELECT id, type, time_created, data FROM session_message WHERE session_id = ? ORDER BY seq`
		pq = `SELECT '', message_id, type, time_created, data FROM session_part WHERE session_id = ? ORDER BY message_id, ordinal`
	}
	rows, err := d.conn.Query(mq, sessionID)
	if err != nil {
		return nil, err
	}
	var out []message
	idx := map[string]int{}
	for rows.Next() {
		var id, typ, data string
		var created int64
		if err := rows.Scan(&id, &typ, &created, &data); err != nil {
			rows.Close()
			return nil, err
		}
		var m message
		_ = json.Unmarshal([]byte(data), &m)
		m.ID = id
		if m.Role == "" {
			m.Role = roleOf(typ)
		}
		if m.Time.Created == 0 {
			m.Time.Created = created
		}
		idx[id] = len(out)
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prow, err := d.conn.Query(pq, sessionID)
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	for prow.Next() {
		var id, msgID, typ, data string
		var created int64
		if err := prow.Scan(&id, &msgID, &typ, &created, &data); err != nil {
			return nil, err
		}
		i, ok := idx[msgID]
		if !ok {
			continue
		}
		var p part
		_ = json.Unmarshal([]byte(data), &p)
		if p.ID == "" {
			p.ID = id
		}
		if p.Type == "" {
			p.Type = typ
		}
		p.created = created
		out[i].parts = append(out[i].parts, p)
	}
	return out, prow.Err()
}

// roleOf maps the message "type" column of the newer layout onto a role.
func roleOf(typ string) string {
	t := strings.ToLower(typ)
	switch {
	case strings.Contains(t, "user"):
		return "user"
	case strings.Contains(t, "assistant"):
		return "assistant"
	}
	return t
}
