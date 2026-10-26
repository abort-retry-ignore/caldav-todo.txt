package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is the on-disk configuration (~/.config/tuxedodav/config.json).
// Paths written here may contain "~" which is expanded on load.
type Config struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Server      string   `json:"server"`      // e.g. https://cloud.021407.xyz
	ListURL     string   `json:"listURL"`     // single-list mode: full CalDAV collection URL
	List        string   `json:"list"`        // single-list mode: collection name
	Lists       []string `json:"lists"`       // multi-list mode: collections merged into one mirror
	DefaultList string   `json:"defaultList"` // where tasks without a +list tag go
	TodoFile    string   `json:"todoFile"`    // mirror file the widget reads
	StateDir    string   `json:"stateDir"`    // sidecar + lock directory
	Interval    int      `json:"interval"`    // daemon poll seconds
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tuxedodav", "config.json"), nil
}

func loadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no config; run: tuxedodav init-config ... (%w)", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("bad config %s: %w", p, err)
	}
	if c.Username == "" || c.Password == "" {
		return nil, fmt.Errorf("config missing username/password: %s", p)
	}
	if _, err := c.collections(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Collection is one CalDAV calendar participating in the merged mirror.
type Collection struct {
	Name string // display name (may contain spaces)
	Slug string // todo.txt token value: spaces -> "_"
	URL  string // collection URL with trailing slash
}

// collections resolves the participating CalDAV collections.
func (c *Config) collections() ([]Collection, error) {
	srv := strings.TrimRight(c.Server, "/")
	if srv != "" && !strings.Contains(srv, "://") {
		srv = "https://" + srv
	}
	mk := func(name string) Collection {
		return Collection{Name: name, Slug: slugify(name),
			URL: fmt.Sprintf("%s/remote.php/dav/calendars/%s/%s/", srv, c.Username, url.PathEscape(name))}
	}
	var out []Collection
	seen := map[string]bool{}
	for _, name := range c.Lists {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, mk(name))
	}
	if len(out) == 0 {
		if c.ListURL != "" {
			name := c.List
			if name == "" {
				name = "list"
			}
			out = append(out, Collection{Name: name, Slug: slugify(name), URL: strings.TrimRight(c.ListURL, "/") + "/"})
		} else if c.List != "" {
			out = append(out, mk(c.List))
		} else {
			return nil, fmt.Errorf("config needs lists[] or list/listURL + server")
		}
	}
	dl := c.DefaultList
	if dl == "" {
		dl = out[0].Name
	}
	for _, col := range out {
		if col.Name == dl {
			return out, nil
		}
	}
	return nil, fmt.Errorf("defaultList %q is not among lists", dl)
}

// todoAndDoneFiles resolve the mirror paths: TODO_FILE/DONE_FILE env wins
// (the omarchy widget sets them), then config, then defaults ~/todo.txt.
func (c *Config) todoAndDoneFiles() (todo, done string) {
	home, _ := os.UserHomeDir()
	todo = c.TodoFile
	if todo == "" {
		todo = filepath.Join(home, "todo.txt")
	}
	todo = expandHome(todo)
	if env := os.Getenv("TODO_FILE"); env != "" {
		todo = env
	}
	done = filepath.Join(filepath.Dir(expandHome(todo)), "done.txt")
	if env := os.Getenv("DONE_FILE"); env != "" {
		done = env
	}
	return todo, done
}

func (c *Config) stateDir() string {
	home, _ := os.UserHomeDir()
	if c.StateDir != "" {
		return expandHome(c.StateDir)
	}
	return filepath.Join(home, ".local", "state", "tuxedodav")
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func saveConfig(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func initConfig(rest []string) error {
	c := &Config{Interval: 60}
	get := func(flag string) (string, error) {
		for i := 0; i < len(rest); i++ {
			if rest[i] == flag {
				if i+1 >= len(rest) {
					return "", fmt.Errorf("%s needs a value", flag)
				}
				return rest[i+1], nil
			}
		}
		return "", nil
	}
	var err error
	if c.Username, err = get("--user"); err != nil {
		return err
	}
	if c.Password, err = get("--password"); err != nil {
		return err
	}
	if c.ListURL, err = get("--url"); err != nil {
		return err
	}
	if c.Server, err = get("--server"); err != nil {
		return err
	}
	if c.List, err = get("--list"); err != nil {
		return err
	}
	if c.TodoFile, err = get("--todo-file"); err != nil {
		return err
	}
	if s, _ := get("--interval"); s != "" {
		if _, e := fmt.Sscanf(s, "%d", &c.Interval); e != nil || c.Interval <= 0 {
			return fmt.Errorf("bad --interval %q", s)
		}
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("init-config requires --user and --password")
	}
	if lis, _ := get("--lists"); lis != "" {
		for _, n := range strings.Split(lis, ",") {
			if n = strings.TrimSpace(n); n != "" {
				c.Lists = append(c.Lists, n)
			}
		}
	}
	if c.DefaultList, err = get("--default-list"); err != nil {
		return err
	}
	if c.ListURL == "" && c.Server == "" {
		c.Server = "https://cloud.021407.xyz"
	}
	if len(c.Lists) == 0 && c.List == "" && c.ListURL == "" {
		return fmt.Errorf(`init-config requires --lists "a,b,c" or --list/--url`)
	}
	if _, err := c.collections(); err != nil {
		return err
	}
	if err := saveConfig(c); err != nil {
		return err
	}
	p, _ := configPath()
	fmt.Printf("config written: %s\n", p)
	return nil
}

// slugify turns a collection name into a todo.txt token value.
func slugify(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
}
