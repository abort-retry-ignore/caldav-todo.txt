package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// sidecar keeps the uid/href order aligned with todo.txt lines.
type sidecar struct {
	Entries []struct {
		UID  string `json:"uid"`
		Href string `json:"href"`
	} `json:"entries"`
}

func (c *Config) ensureStateDir() (string, error) {
	d := c.stateDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// lockFile acquires an exclusive flock on stateDir/lock for the duration of f.
// Every verb and daemon tick runs under this lock, so mirror writes never interleave.
func (c *Config) lockFile(f func() error) error {
	dir, err := c.ensureStateDir()
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return f()
}

func readMirror(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

func writeIfChanged(path, content string) (bool, error) {
	cur, err := readMirror(path)
	if err != nil {
		return false, err
	}
	if cur == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tuxedodav.tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func newUID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tuxedodav-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("tuxedodav-%x", b)
}

// pull refreshes both mirrors + sidecar from the CalDAV server. Caller holds lock.
func pull(cl *Client, c *Config, todoFile, doneFile string) (changed bool, err error) {
	items, err := cl.report()
	if err != nil {
		return false, err
	}

	var open []VTodo
	var doneList []VTodo
	for _, it := range items {
		t := it.Task
		if t.Completed != "" {
			doneList = append(doneList, t)
		} else {
			open = append(open, t)
		}
	}
	sortTasks(open)
	sort.SliceStable(doneList, func(i, j int) bool {
		a, b := doneList[i].Completed, doneList[j].Completed
		if a != b {
			return a < b
		}
		return doneList[i].Uid < doneList[j].Uid
	})

	todoLines := make([]string, 0, len(open))
	side := sidecar{}
	for _, t := range open {
		todoLines = append(todoLines, lineFromTask(t))
		side.Entries = append(side.Entries, struct {
			UID  string `json:"uid"`
			Href string `json:"href"`
		}{t.Uid, hrefOf(items, t.Uid)})
	}
	doneLines := make([]string, 0, len(doneList))
	for _, t := range doneList {
		doneLines = append(doneLines, lineFromTask(t))
	}

	todoStr := ""
	if len(todoLines) > 0 {
		todoStr = joinLines(todoLines)
	}
	doneStr := ""
	if len(doneLines) > 0 {
		doneStr = joinLines(doneLines)
	}

	c1, err := writeIfChanged(todoFile, todoStr)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", todoFile, err)
	}
	c2, err := writeIfChanged(doneFile, doneStr)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", doneFile, err)
	}
	if c3 := writeSidecar(c, side); c3 != nil {
		return false, c3
	}
	return c1 || c2, nil
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

func hrefOf(items []item, uid string) string {
	for _, it := range items {
		if it.UID == uid {
			return it.Href
		}
	}
	return ""
}

func sidecarPath(c *Config) (string, error) {
	dir, err := c.ensureStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "uids.json"), nil
}

func writeSidecar(c *Config, s sidecar) error {
	p, err := sidecarPath(c)
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func readSidecar(c *Config) (sidecar, error) {
	p, err := sidecarPath(c)
	if err != nil {
		return sidecar{}, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return sidecar{}, nil
	}
	if err != nil {
		return sidecar{}, err
	}
	var s sidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return sidecar{}, fmt.Errorf("bad sidecar %s: %w", p, err)
	}
	return s, nil
}

// resolveLine returns the uid/href for 1-based line number n, cross-checking
// that the line's text still matches what the widget saw.
func resolveLine(c *Config, todoFile string, n int) (string, string, error) {
	lines, err := fileLines(todoFile)
	if err != nil {
		return "", "", err
	}
	if n < 1 || n > len(lines) {
		return "", "", fmt.Errorf("line %d out of range (file has %d lines)", n, len(lines))
	}
	s, err := readSidecar(c)
	if err != nil {
		return "", "", err
	}
	if n > len(s.Entries) {
		return "", "", fmt.Errorf("line %d has no sidecar entry; run a sync first", n)
	}
	e := s.Entries[n-1]
	// sanity: uid recorded when the line was written; text may have drifted, that's ok
	if e.UID == "" {
		return "", "", fmt.Errorf("line %d has empty uid", n)
	}
	return e.UID, e.Href, nil
}

func fileLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range splitLines(string(data)) {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

func splitLines(s string) []string { return strings.Split(s, "\n") }
