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
	Username string `json:"username"`
	Password string `json:"password"`
	ListURL  string `json:"listURL"`  // full CalDAV collection URL, trailing slash optional
	Server   string `json:"server"`   // e.g. https://cloud.021407.xyz (used when ListURL empty)
	List     string `json:"list"`     // collection name inside /calendars/<user>/
	TodoFile string `json:"todoFile"` // mirror file the widget reads
	StateDir string `json:"stateDir"` // sidecar + lock directory
	Interval int    `json:"interval"` // daemon poll seconds
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
	if c.ListURL == "" && (c.Server == "" || c.List == "" || c.Username == "") {
		return nil, fmt.Errorf("config needs listURL or server+list+username")
	}
	return &c, nil
}

// collectionURL resolves the CalDAV collection URL.
func (c *Config) collectionURL() (string, error) {
	u := c.ListURL
	if u == "" {
		srv := strings.TrimRight(c.Server, "/")
		if !strings.Contains(srv, "://") {
			srv = "https://" + srv
		}
		u = fmt.Sprintf("%s/remote.php/dav/calendars/%s/%s", srv, c.Username, c.List)
	}
	u = strings.TrimRight(u, "/") + "/"
	if _, err := url.Parse(u); err != nil {
		return "", fmt.Errorf("bad collection URL %q: %w", u, err)
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("collection URL must be http(s), got %q", u)
	}
	return u, nil
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
	if c.ListURL == "" && (c.List == "" || c.Server == "") {
		return fmt.Errorf("init-config requires --url or --server + --list")
	}
	if _, err := c.collectionURL(); err != nil {
		return err
	}
	if err := saveConfig(c); err != nil {
		return err
	}
	p, _ := configPath()
	fmt.Printf("config written: %s\n", p)
	return nil
}
