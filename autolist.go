package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxAutoListsPerPass is a runaway brake for the tag→list normalizer.
const maxAutoListsPerPass = 10

// autoListTarget decides whether a task should be (re)homed into a list named
// after its first tag: returns the tag value (stripped of a leading @/+)
// and the remaining tag set, or "" when no move is warranted.
// known is keyed by LOWERCASED slugs of known collections; a target that
// case-matches a known one is left alone so no twin list spawns.
func autoListTarget(tags []string, curSlug string, known map[string]bool) (string, []string) {
	for len(tags) > 0 {
		first := strings.TrimSpace(tags[0])
		tags = tags[1:]
		if first == "" {
			continue
		}
		name := first
		if len(first) > 1 && (first[0] == '@' || first[0] == '+') {
			name = first[1:]
		}
		if name == "" {
			continue
		}
		slug := slugify(name)
		if strings.EqualFold(slug, curSlug) {
			// already in a list named by this tag
			return "", nil
		}
		if known[strings.ToLower(slug)] {
			// case-variant of a known collection: don't spawn a twin
			return "", nil
		}
		return name, tags
	}
	return "", nil
}

// normalizeTags runs the tag→list invariant over the aggregated items:
// for every task whose first tag names a missing, non-colliding list it
// creates the collection (MKCALENDAR), moves the object there, strips the
// consumed tag, and registers the list. It returns the (possibly replaced)
// items and how many moves happened. Caller holds the flock.
func normalizeTags(c *Config, cl *Client, items []item) ([]item, int, error) {
	if !c.autoListEnabled() {
		return items, 0, nil
	}
	known := map[string]bool{}
	for _, s := range colSlugs(cl.cols) {
		known[strings.ToLower(s)] = true
	}
	for _, e := range c.Excludes {
		known[strings.ToLower(e)] = true // never auto-create an excluded list
	}
	created := 0
	moved := 0
	var createdNames []string
	var warnings []string

	for i := range items {
		it := &items[i]
		name, rest := autoListTarget(it.Task.Categories, it.Slug, known)
		if name == "" {
			continue
		}
		slug := slugify(name)

		// find or create the target collection (case-insensitive slug match:
		// tag "errands" reuses an existing "Errands" list instead of a twin)
		col, found := cl.colBySlugFold(slug)
		if !found {
			if created >= maxAutoListsPerPass {
				warnings = append(warnings, fmt.Sprintf("list cap reached, skipping %q", name))
				continue
			}
			var merr error
			col, merr = cl.mkCol(name)
			if merr != nil {
				warnings = append(warnings, fmt.Sprintf("MKCALENDAR %q: %v", name, merr))
				continue
			}
			created++
			createdNames = append(createdNames, name)
			known[strings.ToLower(slug)] = true
		}

		// move: PUT (categories minus consumed tag) into target, DELETE old
		nt := it.Task
		nt.Categories = rest
		newHref := cl.itemURL(col, nt.Uid)
		if newHref == it.Href {
			continue
		}
		if err := cl.put(newHref, writeICS(nt)); err != nil {
			warnings = append(warnings, fmt.Sprintf("move %q: %v", nt.Summary, err))
			continue
		}
		if err := cl.delete(it.Href); err != nil {
			warnings = append(warnings, fmt.Sprintf("delete old %s: %v", it.Href, err))
		}
		it.Href = newHref
		it.ListName = col.Name
		it.Slug = col.Slug
		it.Task = nt
		moved++
	}

	if len(createdNames) > 0 {
		if err := persistLists(c, createdNames); err != nil {
			warnings = append(warnings, fmt.Sprintf("persist lists: %v", err))
		}
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr(), "tuxedodav: autolist: %s\n", w)
	}
	return items, moved, nil
}

// mkCol creates a VTODO-only calendar collection via MKCALENDAR.
func (cl *Client) mkCol(name string) (Collection, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<mkcalendar xmlns="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
 <set>
  <d:resourcetype><d:collection/><c:calendar/></d:resourcetype>
  <c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set>
  <d:displayname>` + icsEscape(name) + `</d:displayname>
 </set>
</mkcalendar>`
	col := Collection{Name: name, Slug: slugify(name),
		URL: fmt.Sprintf("%s/remote.php/dav/calendars/%s/%s/", strings.TrimSuffix(origin(cl.cols[0].URL), "/"),
			cl.username, url.PathEscape(name))}
	data, status, err := cl.do("MKCALENDAR", col.URL, body, map[string]string{"Content-Type": "application/xml"})
	if err != nil {
		return col, err
	}
	if status != http.StatusCreated && status != http.StatusOK && status != http.StatusMethodNotAllowed {
		return col, fmt.Errorf("HTTP %d: %s", status, trunc(data, 200))
	}
	cl.addCol(col)
	return col, nil
}

// addCol registers a collection for reporting/routing in this process.
func (cl *Client) addCol(col Collection) {
	for _, e := range cl.cols {
		if strings.EqualFold(e.Slug, col.Slug) {
			return
		}
	}
	cl.cols = append(cl.cols, col)
}
