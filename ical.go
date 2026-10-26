package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// VTodo is the server-side task (subset of VTODO properties we care about).
type VTodo struct {
	Uid        string
	Summary    string // plain text (no +proj/@ctx tokens: those live in Categories)
	Priority   int    // 0 = none; 1,5,9 = A,B,C
	Due        string // "2006-01-02" or "" (date-only due)
	Created    string // "2006-01-02" or ""
	Completed  string // "2006-01-02" or ""
	Categories []string
}

// Token is one ordered piece of a todo.txt line body.
type Token struct {
	Kind  string // "word" | "project" | "context" | "due"
	Value string // raw word for words, tag value for typed tokens
}

const timeLayoutICS = "20060102T150405Z"

// ---------- todo.txt line <-> tokens ----------

// parseLine parses a full todo.txt line: [x <compDate>] [(P)] [<createdDate>] body...
func parseLine(line string) (done bool, compDate, prio, createdDate string, tokens []Token, err error) {
	s := strings.TrimSpace(line)
	if strings.HasPrefix(s, "x ") {
		done = true
		s = strings.TrimSpace(s[2:])
		if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
			d := s[:10]
			if _, e := time.Parse("2006-01-02", d); e == nil {
				compDate = d
				s = strings.TrimSpace(s[10:])
			}
		}
	}
	if strings.HasPrefix(s, "(") && len(s) >= 3 && s[2] == ')' && s[1] >= 'A' && s[1] <= 'Z' {
		prio = s[1:2]
		s = strings.TrimSpace(s[3:])
	}
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		d := s[:10]
		if _, e := time.Parse("2006-01-02", d); e == nil {
			createdDate = d
			s = strings.TrimSpace(s[10:])
		}
	}
	tokens = parseTokens(s)
	if len(tokens) == 0 {
		err = fmt.Errorf("empty task line")
	}
	return
}

// parseTokens classifies whitespace-separated words of a line body.
func parseTokens(s string) []Token {
	var out []Token
	for _, w := range strings.Fields(s) {
		switch {
		case len(w) > 1 && strings.HasPrefix(w, "+") && !strings.Contains(w[1:], "+"):
			out = append(out, Token{"project", w[1:]})
		case len(w) > 1 && strings.HasPrefix(w, "@"):
			out = append(out, Token{"context", w[1:]})
		case strings.HasPrefix(w, "due:") && isDate(w[4:]):
			out = append(out, Token{"due", w[4:]})
		default:
			out = append(out, Token{"word", w})
		}
	}
	return out
}

func isDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// summaryFromTokens returns the plain-text summary (typed tokens stripped).
func summaryFromTokens(tokens []Token) string {
	words := []string{}
	for _, t := range tokens {
		if t.Kind == "word" {
			words = append(words, t.Value)
		}
	}
	return strings.Join(words, " ")
}

// lineFromTask renders a todo.txt line from a VTodo.
// Canonical order: [x <comp>] [(P)] [<created>] words... +proj @ctx due:date
func lineFromTask(t VTodo) string {
	var b strings.Builder
	if t.Completed != "" {
		b.WriteString("x " + t.Completed + " ")
	}
	if p := prioLetter(t.Priority); p != "" {
		b.WriteString("(" + p + ") ")
	}
	if t.Created != "" {
		b.WriteString(t.Created + " ")
	}
	b.WriteString(t.Summary)
	for _, cat := range t.Categories {
		if strings.HasPrefix(cat, "+") {
			b.WriteString(" " + cat)
		} else if strings.HasPrefix(cat, "@") {
			b.WriteString(" " + cat)
		} else {
			b.WriteString(" @" + cat)
		}
	}
	if t.Due != "" {
		b.WriteString(" due:" + t.Due)
	}
	return strings.TrimSpace(b.String())
}

// taskFromParsedLine builds the VTODO fields from a parsed line (for add/replace).
func taskFromParsedLine(done bool, compDate, prio, createdDate string, tokens []Token, base *VTodo) VTodo {
	t := VTodo{}
	if base != nil {
		t = *base
	}
	t.Summary = summaryFromTokens(tokens)
	t.Priority = prioNumber(prio)
	t.Created = createdDate
	t.Due = ""
	var cats []string
	for _, tok := range tokens {
		switch tok.Kind {
		case "due":
			t.Due = tok.Value
		case "project":
			cats = append(cats, "+"+tok.Value)
		case "context":
			cats = append(cats, "@"+tok.Value)
		}
	}
	t.Categories = cats
	if done {
		t.Completed = compDate
	} else {
		t.Completed = ""
	}
	return t
}

func prioLetter(p int) string {
	switch p {
	case 1:
		return "A"
	case 5:
		return "B"
	case 9:
		return "C"
	}
	return ""
}

func prioNumber(letter string) int {
	switch letter {
	case "A":
		return 1
	case "B":
		return 5
	case "C":
		return 9
	}
	return 0
}

// ---------- iCalendar ----------

// icsEscape escapes TEXT property values per RFC 5545.
func icsEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\n", "\\n", ";", "\\;", ",", "\\,")
	return r.Replace(s)
}

func icsUnescape(s string) string {
	r := strings.NewReplacer("\\n", "\n", "\\N", "\n", "\\;", ";", "\\,", ",", "\\\\", "\\")
	return r.Replace(s)
}

// splitCategories splits a CATEGORIES value on unescaped commas.
func splitCategories(v string) []string {
	var parts []string
	cur := strings.Builder{}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '\\' && i+1 < len(v) {
			cur.WriteByte(c)
			i++
			cur.WriteByte(v[i])
			continue
		}
		if c == ',' {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	parts = append(parts, cur.String())
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// foldLine wraps physical lines at 75 octets (RFC 5545 3.1).
func foldLine(line string) string {
	if len(line) <= 75 {
		return line
	}
	var out strings.Builder
	out.WriteString(line[:75])
	rest := line[75:]
	for len(rest) > 74 {
		out.WriteString("\r\n " + rest[:74])
		rest = rest[74:]
	}
	if rest != "" {
		out.WriteString("\r\n " + rest)
	}
	return out.String()
}

func propString(name string, val string, valueParam string) string {
	p := name
	if valueParam != "" {
		p += ";" + valueParam
	}
	return foldLine(p+":"+icsEscape(val)) + "\r\n"
}

// parseICS extracts the first VTODO component.
func parseICS(data string) (*VTodo, error) {
	// unfold
	lines := []string{}
	for _, raw := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		if (strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
			continue
		}
		if strings.TrimSpace(raw) != "" {
			lines = append(lines, raw)
		}
	}
	in := false
	t := &VTodo{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		up := strings.ToUpper(trimmed)
		switch {
		case up == "BEGIN:VTODO":
			in = true
			t = &VTodo{}
		case up == "END:VTODO":
			return t, nil
		case in:
			idx := strings.Index(trimmed, ":")
			if idx < 0 {
				continue
			}
			head, val := trimmed[:idx], trimmed[idx+1:]
			name := strings.ToUpper(head)
			if j := strings.IndexAny(name, ";"); j >= 0 {
				name = name[:j]
			}
			val = strings.TrimSpace(val)
			switch name {
			case "UID":
				t.Uid = icsUnescape(val)
			case "SUMMARY":
				t.Summary = icsUnescape(val)
			case "PRIORITY":
				n, _ := strconv.Atoi(val)
				t.Priority = n
			case "DUE":
				t.Due = dateFromICSDateTime(val)
			case "CREATED", "DTSTART":
				if t.Created == "" {
					t.Created = dateFromICSDateTime(val)
				}
			case "COMPLETED":
				t.Completed = dateFromICSDateTime(val)
			case "CATEGORIES":
				for _, p := range splitCategories(val) {
					t.Categories = append(t.Categories, icsUnescape(p))
				}
			case "STATUS":
				if strings.EqualFold(val, "COMPLETED") && t.Completed == "" {
					t.Completed = today()
				}
			}
		}
	}
	if !in {
		return nil, fmt.Errorf("no VTODO component in ICS data")
	}
	return t, nil
}

// dateFromICSDateTime renders any ICS date/date-time (20261103, 20261103T090000Z,
// floating, pre-dashed) as "2006-01-02".
func dateFromICSDateTime(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, "Z")
	if m, err := time.Parse("2006-01-02", v); err == nil {
		return m.Format("2006-01-02")
	}
	if len(v) == 8 {
		if m, err := time.Parse("20060102", v); err == nil {
			return m.Format("2006-01-02")
		}
	}
	if len(v) >= 15 && v[8] == 'T' {
		if m, err := time.Parse("20060102", v[:8]); err == nil {
			return m.Format("2006-01-02")
		}
	}
	return v
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

// writeICS renders a full VCALENDAR with one VTODO.
func writeICS(t VTodo) string {
	if t.Uid == "" {
		t.Uid = "tuxedodav-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//tuxedodav//EN\r\n")
	b.WriteString("BEGIN:VTODO\r\n")
	b.WriteString(propString("UID", t.Uid, ""))
	b.WriteString("DTSTAMP:" + time.Now().UTC().Format(timeLayoutICS) + "\r\n")
	if t.Created != "" {
		b.WriteString(propString("CREATED", t.Created, "VALUE=DATE"))
	}
	b.WriteString(propString("SUMMARY", t.Summary, ""))
	if t.Priority > 0 {
		b.WriteString("PRIORITY:" + strconv.Itoa(t.Priority) + "\r\n")
	}
	if len(t.Categories) > 0 {
		esc := make([]string, len(t.Categories))
		for i, cat := range t.Categories {
			esc[i] = icsEscape(cat)
		}
		b.WriteString(foldLine("CATEGORIES:"+strings.Join(esc, ",")) + "\r\n")
	}
	if t.Due != "" {
		b.WriteString(propString("DUE", strings.ReplaceAll(t.Due, "-", ""), "VALUE=DATE"))
	}
	if t.Completed != "" {
		b.WriteString("STATUS:COMPLETED\r\n")
		b.WriteString(propString("COMPLETED", strings.ReplaceAll(t.Completed, "-", "")+"T000000Z", ""))
	} else {
		b.WriteString("STATUS:NEEDS-ACTION\r\n")
	}
	b.WriteString("END:VTODO\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// sortTasks orders incomplete tasks for a stable mirror file:
// due asc (none last), priority asc (none last), created asc, uid.
func sortTasks(tasks []VTodo) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if (a.Due == "") != (b.Due == "") {
			return b.Due == ""
		}
		if a.Due != b.Due && a.Due != "" && b.Due != "" {
			return a.Due < b.Due
		}
		pa, pb := prioSortKey(a.Priority), prioSortKey(b.Priority)
		if pa != pb {
			return pa < pb
		}
		if (a.Created == "") != (b.Created == "") {
			return b.Created == ""
		}
		if a.Created != b.Created && a.Created != "" && b.Created != "" {
			return a.Created < b.Created
		}
		return a.Uid < b.Uid
	})
}

func prioSortKey(p int) int {
	switch p {
	case 1:
		return 1
	case 5:
		return 2
	case 9:
		return 3
	}
	return 4
}
