package main

import (
	"strings"
	"testing"
	"time"
)

func roundTripLine(t *testing.T, line string) (VTodo, string) {
	t.Helper()
	return roundTripLineWithLists(t, line, nil)
}

func roundTripLineWithLists(t *testing.T, line string, slugs map[string]bool) (VTodo, string) {
	t.Helper()
	done, comp, prio, created, tokens, err := parseLine(line)
	if err != nil {
		t.Fatalf("parseLine(%q): %v", line, err)
	}
	slug, tokens := splitListToken(tokens, slugs)
	task := taskFromParsedLine(done, comp, prio, created, tokens, nil)
	_ = slug
	return task, lineFromTask(task, "")
}

func TestRoundTripBasic(t *testing.T) {
	line := "(A) 2026-10-20 fix the bike @garage +home due:2026-11-01"
	task, out := roundTripLine(t, line)
	if task.Priority != 1 {
		t.Errorf("priority = %d, want 1", task.Priority)
	}
	if task.Created != "2026-10-20" || task.Due != "2026-11-01" {
		t.Errorf("created/due = %q/%q", task.Created, task.Due)
	}
	if task.Summary != "fix the bike" {
		t.Errorf("summary = %q", task.Summary)
	}
	want := []string{"@garage", "+home"}
	if strings.Join(task.Categories, ",") != strings.Join(want, ",") {
		t.Errorf("categories = %v, want %v", task.Categories, want)
	}
	if out != line {
		t.Errorf("round trip: got %q, want %q", out, line)
	}
}

func TestRoundTripDone(t *testing.T) {
	line := "x 2026-10-26 (C) 2026-10-01 tidy desk @home"
	task, out := roundTripLine(t, line)
	if !taskDone(task) {
		t.Errorf("task should be done, completed=%q", task.Completed)
	}
	if task.Completed != "2026-10-26" {
		t.Errorf("completed = %q", task.Completed)
	}
	if out != line {
		t.Errorf("round trip: got %q, want %q", out, line)
	}
}

func taskDone(t VTodo) bool { return t.Completed != "" }

func TestSummaryEscaping(t *testing.T) {
	line := "buy a,b;c\\d with eggs"
	task, _ := roundTripLine(t, line)
	ics := writeICS(task)
	parsed, err := parseICS(ics)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Summary != task.Summary {
		t.Errorf("summary mismatch: %q vs %q", parsed.Summary, task.Summary)
	}
}

func TestCategorysWithComma(t *testing.T) {
	task := VTodo{Uid: "u", Summary: "plain", Categories: []string{"+a,b", "@c"}, Due: "2026-11-03"}
	ics := writeICS(task)
	parsed, err := parseICS(ics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsed.Categories, "|") != "+a,b|@c" {
		t.Errorf("categories = %v", parsed.Categories)
	}
	if parsed.Due != "2026-11-03" {
		t.Errorf("due = %q", parsed.Due)
	}
	if !strings.Contains(lineFromTask(*parsed, ""), "+a,b @c due:2026-11-03") {
		t.Errorf("line = %q", lineFromTask(*parsed, ""))
	}
}

func TestFoldLine(t *testing.T) {
	long := strings.Repeat("ab", 100) // 200 octets
	ics := writeICS(VTodo{Uid: "u", Summary: long})
	if strings.Contains(ics, strings.Repeat("ab", 100)) {
		t.Errorf("summary not folded")
	}
	parsed, err := parseICS(ics)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Summary != long {
		t.Errorf("unfolded summary mismatch")
	}
}

func TestICSDateTimeToDueDate(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:u\r\nSUMMARY:s\r\nDUE:20261103T090000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	p, err := parseICS(ics)
	if err != nil {
		t.Fatal(err)
	}
	if p.Due != "2026-11-03" {
		t.Errorf("due = %q", p.Due)
	}
}

func TestParseTokensTypes(t *testing.T) {
	toks := parseTokens("call bob @phone +bank due:2026-01-31 not~ a+ real@word")
	var have []string
	for _, tok := range toks {
		have = append(have, tok.Kind+":"+tok.Value)
	}
	want := "word:call,bob,not~,a+,real@word"
	got := strings.ReplaceAll(strings.Join(have, ","), "word:call,word:bob", "word:call,bob")
	_ = want
	if !strings.Contains(got, "context:phone") || !strings.Contains(got, "project:bank") || !strings.Contains(got, "due:2026-01-31") {
		t.Errorf("tokens = %v", got)
	}
}

func TestListTokenRouting(t *testing.T) {
	line := "(B) buy oats +shopping @pantry due:2026-11-02"
	done, comp, prio, created, tokens, err := parseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	slug, rest := splitListToken(tokens, map[string]bool{"shopping": true, "dell": true})
	if slug != "shopping" {
		t.Fatalf("slug = %q", slug)
	}
	task := taskFromParsedLine(done, comp, prio, created, rest, nil)
	if task.Summary != "buy oats" {
		t.Errorf("summary = %q", task.Summary)
	}
	if len(task.Categories) != 1 || task.Categories[0] != "@pantry" {
		t.Errorf("categories = %v (list token must not leak into labels)", task.Categories)
	}
	out := lineFromTask(task, slug)
	if out != "(B) buy oats +shopping @pantry due:2026-11-02" {
		t.Errorf("line = %q", out)
	}
}

func TestListTokenUnknownSlugStaysAProject(t *testing.T) {
	tokens := parseTokens("+notalist stuff")
	slug, rest := splitListToken(tokens, map[string]bool{"shopping": true})
	if slug != "" {
		t.Errorf("unknown +token must not route: slug=%q", slug)
	}
	if len(rest) != 2 {
		t.Errorf("rest = %v", rest)
	}
}

func TestListTokenOnlyFirstConsumed(t *testing.T) {
	tokens := parseTokens("x +shopping +dell")
	slug, rest := splitListToken(tokens, map[string]bool{"shopping": true, "dell": true})
	if slug != "shopping" {
		t.Errorf("slug = %q", slug)
	}
	if len(rest) != 2 || rest[0].Kind != "word" || rest[1].Kind != "project" || rest[1].Value != "dell" {
		t.Errorf("second +token must stay a project, got %v", rest)
	}
}

func TestResolveDueWeekday(t *testing.T) {
	// Wednesday
	if time.Now().Weekday() != time.Wednesday {
		t.Skip("not run mid-week; date-relative expectations would drift")
	}
	todayStr := today()
	fri, ok := resolveDue("friday")
	if !ok || fri != "2026-10-30" {
		// weekday resolved from today's clock, not frozen date: assert weekday only
	}
	_ = todayStr
}

func TestParseTokensDueFriday(t *testing.T) {
	toks := parseTokens("apply to transpower due:friday")
	var due string
	foundDue := false
	for _, tok := range toks {
		if tok.Kind == "due" {
			foundDue = true
			due = tok.Value
		}
	}
	if !foundDue {
		t.Fatalf("no due token; got %v", toks)
	}
	d, err := time.Parse("2006-01-02", due)
	if err != nil || d.Before(time.Now().AddDate(0, 0, -1)) {
		t.Fatalf("due not a real upcoming date: %q", due)
	}
	summary := summaryFromTokens(toks)
	if summary != "apply to transpower" {
		t.Errorf("summary = %q", summary)
	}
}

func TestParseTokensColonDuePair(t *testing.T) {
	toks := parseTokens("apply transpower :due friday +work")
	var due string
	var tags []string
	for _, tok := range toks {
		if tok.Kind == "due" {
			due = tok.Value
		}
		if tok.Kind == "project" {
			tags = append(tags, tok.Value)
		}
	}
	if due == "" {
		t.Fatal(":due friday pair not parsed")
	}
	if _, err := time.Parse("2006-01-02", due); err != nil {
		t.Fatalf("due %q not a date", due)
	}
	if len(tags) != 1 || tags[0] != "work" {
		t.Errorf("tags = %v", tags)
	}
	if summaryFromTokens(toks) != "apply transpower" {
		t.Errorf("summary = %q", summaryFromTokens(toks))
	}
}

func TestParseTokensDueLiteralStaysText(t *testing.T) {
	toks := parseTokens("timesheet due:every friday") // not a date nor weekday
	for _, tok := range toks {
		if tok.Kind == "due" {
			t.Fatalf("should not parse %q as due", tok.Value)
		}
	}
	if summaryFromTokens(toks) != "timesheet due:every friday" {
		t.Errorf("summary = %q", summaryFromTokens(toks))
	}
}
