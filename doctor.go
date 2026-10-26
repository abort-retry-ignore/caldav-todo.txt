package main

import (
	"fmt"
	"time"
)

// doctor runs an end-to-end round trip against the configured CalDAV list:
// REPORT -> create -> REPORT (present?) -> GET -> DELETE -> REPORT (gone?).
func doctor() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	fmt.Printf("doctor: lists = %v (default: %s)\n", colSlugs(cl.cols), cl.defaultCol().Slug)

	for _, col := range cl.cols {
		items, err := cl.reportCol(col)
		if err != nil {
			return fmt.Errorf("REPORT %s failed: %w", col.Name, err)
		}
		fmt.Printf("  %-12s REPORT ok, %d VTODOs\n", col.Name, len(items))
	}
	col := cl.defaultCol()
	uid := "tuxedodav-doctor-" + fmt.Sprintf("%d", time.Now().UnixNano())
	test := VTodo{Uid: uid, Summary: "tuxedodav doctor test \\, ; , \\ task", Priority: 5, Due: today(), Categories: []string{"+doctor", "@dell"}, Created: today()}
	href := cl.itemURL(col, uid)
	_ = href
	fmt.Printf("  round-trip in %s...\n", col.Name)
	if err := cl.put(href, writeICS(test)); err != nil {
		return fmt.Errorf("PUT failed: %w", err)
	}
	fmt.Printf("  PUT ok (%s)\n", href)

	items, err := cl.reportCol(col)
	var found *item
	for i := range items {
		if items[i].UID == uid {
			found = &items[i]
		}
	}
	if found == nil {
		_ = cl.delete(href)
		return fmt.Errorf("created task not visible in REPORT")
	}
	if found.Task.Summary != test.Summary {
		_ = cl.delete(href)
		return fmt.Errorf("round-trip mismatch: summary %q != %q", found.Task.Summary, test.Summary)
	}
	if _, err := cl.get(href); err != nil {
		_ = cl.delete(href)
		return fmt.Errorf("GET failed: %w", err)
	}
	fmt.Println("  GET ok, summary round-trips")

	if err := cl.delete(href); err != nil {
		return fmt.Errorf("DELETE failed: %w", err)
	}
	items2, _ := cl.reportCol(col)
	for _, it := range items2 {
		if it.UID == uid {
			return fmt.Errorf("deleted task still visible in REPORT")
		}
	}
	fmt.Println("  DELETE ok")
	if c.autoListEnabled() {
		fmt.Println("autoList: enabled (a tag on a task creates/moves-to a matching list during sync)")
	}
	fmt.Println("doctor: PASS")
	return nil
}

func trimSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
