// ptysmoke runs a binary in a real pty, feeds its output to a terminal
// emulator and prints what the screen shows after each scripted step.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func main() {
	bin := os.Args[1]
	cols, rows := 110, 34
	if len(os.Args) > 2 && os.Args[2] == "small" {
		cols, rows = 80, 24
	}
	steps := os.Args[3:]
	tmp, _ := os.MkdirTemp("", "ts-smoke")
	defer os.RemoveAll(tmp)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "LANG=en_US.UTF-8",
		"XDG_CONFIG_HOME="+tmp+"/c", "XDG_STATE_HOME="+tmp+"/s", "XDG_DATA_HOME="+tmp+"/d")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		panic(err)
	}
	em := vt.NewEmulator(cols, rows)
	var mu sync.Mutex
	go func() { // terminal replies (capability queries) go back to the program
		buf := make([]byte, 4096)
		for {
			n, err := em.Read(buf)
			if err != nil {
				return
			}
			f.Write(buf[:n])
		}
	}()
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				mu.Lock()
				em.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	dump := func(label string) {
		mu.Lock()
		s := em.String()
		mu.Unlock()
		lines := strings.Split(s, "\n")
		fmt.Printf("===== %s =====\n", label)
		for _, l := range lines {
			fmt.Println(strings.TrimRight(l, " "))
		}
	}
	time.Sleep(2 * time.Second)
	dump("start")
	for _, st := range steps {
		// "key:xyz" sends keys, "wait:N" sleeps, "dump:label" prints the screen
		kind, arg, _ := strings.Cut(st, ":")
		switch kind {
		case "key":
			arg = strings.NewReplacer("<enter>", "\r", "<esc>", "\x1b", "<tab>", "\t", "<up>", "\x1b[A", "<down>", "\x1b[B", "<right>", "\x1b[C", "<left>", "\x1b[D", "<space>", " ").Replace(arg)
			io.WriteString(f, arg)
			time.Sleep(400 * time.Millisecond)
		case "wait":
			var n int
			fmt.Sscanf(arg, "%d", &n)
			time.Sleep(time.Duration(n) * time.Millisecond)
		case "dump":
			dump(arg)
		}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		fmt.Println("exited:", err)
	case <-time.After(3 * time.Second):
		fmt.Println("STILL RUNNING after q; killing")
		cmd.Process.Kill()
	}
}
