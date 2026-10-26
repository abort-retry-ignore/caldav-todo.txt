package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal CalDAV VTODO client (REPORT/GET/PUT/DELETE) over one
// user's collections (multi-list mode) or a single collection (legacy mode).
type Client struct {
	cols        []Collection
	defaultSlug string
	username    string
	password    string
	http        *http.Client
}

func newClient(c *Config) (*Client, error) {
	cols, err := c.collections()
	if err != nil {
		return nil, err
	}
	dl := c.DefaultList
	if dl == "" {
		dl = cols[0].Name
	}
	def := slugify(dl)
	for _, col := range cols {
		if col.Name == dl {
			def = col.Slug
			break
		}
	}
	return &Client{cols: cols, defaultSlug: def, username: c.Username, password: c.Password,
		http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (cl *Client) colBySlug(slug string) (Collection, error) {
	for _, col := range cl.cols {
		if col.Slug == slug {
			return col, nil
		}
	}
	return Collection{}, fmt.Errorf("unknown list %q (configured: %v)", slug, colSlugs(cl.cols))
}

func (cl *Client) defaultCol() Collection {
	for _, col := range cl.cols {
		if col.Slug == cl.defaultSlug {
			return col
		}
	}
	return cl.cols[0]
}

func colSlugs(cols []Collection) []string {
	var out []string
	for _, col := range cols {
		out = append(out, col.Slug)
	}
	return out
}

func (cl *Client) do(method, url string, body string, extraHeaders map[string]string) ([]byte, int, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(cl.username, cl.password)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/xml")
	}
	resp, err := cl.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

type item struct {
	UID      string
	Href     string // absolute URL
	Etag     string
	ICS      string
	Task     VTodo
	ListName string
	Slug     string
}

// report runs the VTODO calendar-query REPORT against every collection and
// aggregates the results.
func (cl *Client) report() ([]item, error) {
	var out []item
	var failures []string
	for _, col := range cl.cols {
		items, err := cl.reportCol(col)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", col.Name, err))
			continue
		}
		out = append(out, items...)
	}
	if len(failures) == len(cl.cols) && len(cl.cols) > 0 {
		return nil, fmt.Errorf("all collections failed: %s", strings.Join(failures, "; "))
	}
	return out, nil
}

func (cl *Client) reportCol(col Collection) ([]item, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data/></d:prop>
  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VTODO"/></c:comp-filter></c:filter>
</c:calendar-query>`
	data, status, err := cl.do("REPORT", col.URL, body, map[string]string{"Depth": "1"})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("collection not found: %s", col.URL)
	}
	if status != http.StatusMultiStatus {
		return nil, fmt.Errorf("REPORT %s: HTTP %d: %s", col.URL, status, trunc(data, 300))
	}
	var ms struct {
		Responses []struct {
			Href     string `xml:"href"`
			Propstat []struct {
				Status string `xml:"status"`
				Prop   struct {
					Getetag      string `xml:"getetag"`
					CalendarData string `xml:"calendar-data"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("REPORT parse: %w", err)
	}
	var out []item
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, "200") || ps.Prop.CalendarData == "" {
				continue
			}
			href := r.Href
			if !strings.HasPrefix(href, "http") {
				href = origin(col.URL) + href
			}
			href = strings.TrimSuffix(href, "/")
			t, perr := parseICS(ps.Prop.CalendarData)
			if perr != nil {
				return nil, fmt.Errorf("parse VTODO %s: %w", href, perr)
			}
			if t.Uid == "" {
				continue
			}
			out = append(out, item{UID: t.Uid, Href: href, Etag: ps.Prop.Getetag,
				ICS: ps.Prop.CalendarData, Task: *t, ListName: col.Name, Slug: col.Slug})
		}
	}
	return out, nil
}

// get fetches a single VTODO by href.
func (cl *Client) get(href string) (*VTodo, error) {
	data, status, err := cl.do("GET", href, "", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", href, status, trunc(data, 300))
	}
	return parseICS(string(data))
}

// put creates or replaces the VTODO at href.
func (cl *Client) put(href, ics string) error {
	_, status, err := cl.do("PUT", href, ics, map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("PUT %s: HTTP %d", href, status)
	}
	return nil
}

func (cl *Client) delete(href string) error {
	_, status, err := cl.do("DELETE", href, "", nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK && status != http.StatusNotFound {
		return fmt.Errorf("DELETE %s: HTTP %d", href, status)
	}
	return nil
}

// itemURL builds the object URL for uid inside a collection.
func (cl *Client) itemURL(col Collection, uid string) string {
	return strings.TrimSuffix(col.URL, "/") + "/" + uid + ".ics"
}

// origin returns scheme://host of a URL.
func origin(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return ""
	}
	rest := u[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[:j]
	}
	return u[:i+3] + rest
}

func trunc(b []byte, n int) string {
	s := string(b)
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// slugSet is the set of configured collection slugs used to route +List tokens.
func (cl *Client) slugSet() map[string]bool {
	set := map[string]bool{}
	for _, s := range colSlugs(cl.cols) {
		set[s] = true
	}
	return set
}
