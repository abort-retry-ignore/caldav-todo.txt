package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal CalDAV VTODO client (REPORT/GET/PUT/DELETE).
type Client struct {
	base     string // collection URL with trailing slash
	username string
	password string
	http     *http.Client
}

func newClient(c *Config) (*Client, error) {
	base, err := c.collectionURL()
	if err != nil {
		return nil, err
	}
	return &Client{base: base, username: c.Username, password: c.Password,
		http: &http.Client{Timeout: 30 * time.Second}}, nil
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
	UID  string
	Href string // absolute URL
	Etag string
	ICS  string
	Task VTodo
}

// report runs a calendar-query REPORT for VTODOs in the collection.
func (cl *Client) report() ([]item, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data/></d:prop>
  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VTODO"/></c:comp-filter></c:filter>
</c:calendar-query>`
	data, status, err := cl.do("REPORT", cl.base, body, map[string]string{"Depth": "1"})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("collection not found: %s", cl.base)
	}
	if status != http.StatusMultiStatus {
		return nil, fmt.Errorf("REPORT %s: HTTP %d: %s", cl.base, status, trunc(data, 300))
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
				href = origin(cl.base) + href
			}
			href = strings.TrimSuffix(href, "/")
			t, perr := parseICS(ps.Prop.CalendarData)
			if perr != nil {
				return nil, fmt.Errorf("parse VTODO %s: %w", href, perr)
			}
			if t.Uid == "" {
				continue
			}
			out = append(out, item{UID: t.Uid, Href: href, Etag: ps.Prop.Getetag, ICS: ps.Prop.CalendarData, Task: *t})
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
	s := string(bytes.TrimSpace(b))
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}
