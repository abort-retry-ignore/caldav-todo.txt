package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func stderr() *os.File { return os.Stderr }

// refresh runs a pull while holding the lock.
func refresh(cl *Client, c *Config, todoFile, doneFile string) (bool, error) {
	var changed bool
	err := c.lockFile(func() error {
		var e error
		changed, e = pull(cl, c, todoFile, doneFile)
		return e
	})
	return changed, err
}

func cmdAdd(line string) error {
	if strings.TrimSpace(line) == "" {
		return fmt.Errorf("empty task line")
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	return c.lockFile(func() error {
		done, comp, prio, created, tokens, perr := parseLine(line)
		if perr != nil {
			return perr
		}
		if created == "" {
			created = today()
		}
		uid := newUID()
		t := taskFromParsedLine(done, comp, prio, created, tokens, nil)
		t.Uid = uid
		href := strings.TrimSuffix(cl.base, "/") + "/" + uid + ".ics"
		if perr := cl.put(href, writeICS(t)); perr != nil {
			return perr
		}
		if _, rerr := pull(cl, c, todoFile, doneFile); rerr != nil {
			return rerr
		}
		say("added:", line)
		return nil
	})
}

// applyToLines resolves each given line number against the current mirror,
// fetches the server copy, applies fn, and PUTs it back.
func applyToLines(c *Config, cl *Client, todoFile, doneFile string, nums []int, action string, fn func(*VTodo)) error {
	for _, n := range nums {
		err := c.lockFile(func() error {
			_, href, rerr := resolveLine(c, todoFile, n)
			if rerr != nil {
				return rerr
			}
			cur, gerr := cl.get(href)
			if gerr != nil {
				return gerr
			}
			fn(cur)
			if perr := cl.put(href, writeICS(*cur)); perr != nil {
				return perr
			}
			if _, rerr := pull(cl, c, todoFile, doneFile); rerr != nil {
				return rerr
			}
			say(action, strconv.Itoa(n))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func cmdDo(nums []int) error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	return applyToLines(c, cl, todoFile, doneFile, nums, "completed", func(t *VTodo) {
		t.Completed = dateFromICSDateTime(time.Now().UTC().Format(timeLayoutICS))
		if t.Created == "" {
			t.Created = today()
		}
	})
}

func cmdDel(nums []int) error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	for _, n := range nums {
		err := c.lockFile(func() error {
			_, href, rerr := resolveLine(c, todoFile, n)
			if rerr != nil {
				return rerr
			}
			if rerr = cl.delete(href); rerr != nil {
				return rerr
			}
			if _, rerr := pull(cl, c, todoFile, doneFile); rerr != nil {
				return rerr
			}
			say("deleted", strconv.Itoa(n))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func cmdReplace(n int, newline string) error {
	if strings.TrimSpace(newline) == "" {
		return fmt.Errorf("empty replacement line")
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	return c.lockFile(func() error {
		_, href, rerr := resolveLine(c, todoFile, n)
		if rerr != nil {
			return rerr
		}
		cur, gerr := cl.get(href)
		if gerr != nil {
			return gerr
		}
		done, comp, prio, created, tokens, perr := parseLine(newline)
		if perr != nil {
			return perr
		}
		// preserve server-side fields the line syntax cannot express
		if created == "" {
			created = cur.Created
		}
		nt := taskFromParsedLine(done, comp, prio, created, tokens, cur)
		nt.Uid = cur.Uid
		if !done {
			nt.Completed = ""
		}
		if perr := cl.put(href, writeICS(nt)); perr != nil {
			return perr
		}
		if _, rerr := pull(cl, c, todoFile, doneFile); rerr != nil {
			return rerr
		}
		say("replaced", strconv.Itoa(n))
		return nil
	})
}

func syncOnce() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	changed, err := refresh(cl, c, todoFile, doneFile)
	if err != nil {
		return err
	}
	fmt.Printf("sync %s: changed=%v\n", todoFile, changed)
	return nil
}

func runDaemon(interval int) error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	cl, err := newClient(c)
	if err != nil {
		return err
	}
	todoFile, doneFile := c.todoAndDoneFiles()
	if _, err := c.ensureStateDir(); err != nil {
		return err
	}
	fmt.Fprintf(stderr(), "tuxedodav daemon: %s (+done.txt) <- %s every %ds\n", todoFile, cl.base, interval)
	for {
		var changed bool
		_ = c.lockFile(func() error {
			var e error
			changed, e = pull(cl, c, todoFile, doneFile)
			if e != nil {
				fmt.Fprintf(stderr(), "tuxedodav: sync error: %v\n", e)
			}
			return nil // keep looping on errors (server may be briefly down)
		})
		if changed && os.Getenv("TUXEDODAV_QUIET") == "" {
			fmt.Fprintf(stderr(), "tuxedodav: mirror updated %s\n", time.Now().Format("15:04:05"))
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}
func say(parts ...string) {
	fmt.Println(strings.Join(parts, " "))
}
