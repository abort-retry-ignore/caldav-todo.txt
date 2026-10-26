// Package main implements tuxedodav: a tuxedo CLI shim backed by CalDAV (VTODO).
//
// Commands:
//
//	sync                daemon: pull CalDAV list -> todo.txt/done.txt mirrors
//	sync --once         single pull and exit
//	add <line>          create task from todo.txt line
//	do <n> [m ...]      complete task(s) by 1-based line number
//	replace <n> <line>  edit task by line number
//	del <n> [m ...] [-f] delete task(s) by line number (-f is accepted and ignored)
//	doctor              end-to-end connectivity test (REPORT/PUT/GET/DELETE)
//	init-config         write the config file
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func usage() {
	fmt.Fprint(os.Stderr, `tuxedodav - tuxedo CLI shim backed by CalDAV (VTODO)

usage:
  tuxedodav sync [--interval 60] [--once]
  tuxedodav add <todo.txt line>
  tuxedodav do <lineNumber> [more...]
  tuxedodav replace <lineNumber> <new line>
  tuxedodav del <lineNumber> [more...] [-f]
  tuxedodav doctor
  tuxedodav init-config --user <name> --password <app-password> [--list <name>] [--server <url>] [--url <full collection url>] [--todo-file <path>]
`)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "sync":
		once, interval := parseSyncFlags(rest)
		if once {
			err = syncOnce()
		} else {
			err = runDaemon(interval)
		}
	case "add":
		err = cmdAdd(strings.Join(rest, " "))
	case "do":
		nums, _ := parseNums(rest)
		err = cmdDo(nums)
	case "replace":
		if len(rest) < 2 {
			err = fmt.Errorf("replace needs <lineNumber> <new line>")
		} else {
			n, perr := strconv.Atoi(rest[0])
			if perr != nil {
				err = fmt.Errorf("bad line number %q", rest[0])
			} else {
				err = cmdReplace(n, strings.Join(rest[1:], " "))
			}
		}
	case "del":
		nums, _ := parseNums(rest) // trailing -f accepted and ignored
		err = cmdDel(nums)
	case "doctor":
		err = doctor()
	case "init-config":
		err = initConfig(rest)
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuxedodav: %v\n", err)
		os.Exit(1)
	}
}

func parseSyncFlags(rest []string) (once bool, interval int) {
	interval = 60
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--once":
			once = true
		case "--interval":
			if i+1 < len(rest) {
				if v, e := strconv.Atoi(rest[i+1]); e == nil && v > 0 {
					interval = v
				}
				i++
			}
		}
	}
	return once, interval
}

// parseNums reads 1-based line numbers from args, skipping flags like -f.
func parseNums(rest []string) ([]int, error) {
	var nums []int
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			continue
		}
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("bad line number %q", a)
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return nil, fmt.Errorf("no line numbers given")
	}
	return nums, nil
}
