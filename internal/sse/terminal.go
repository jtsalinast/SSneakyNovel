package sse

import (
	"bufio"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

// logEntryEcho matches the lines logEntry already prints, so mirroring stdout
// does not duplicate every task log line in the panel.
var logEntryEcho = regexp.MustCompile(`^\s*\[(info|warn|error|success|step|debug)\] `)

// CaptureTerminal mirrors everything the process writes to stdout/stderr into
// the Log panel, exactly like the terminal the author is watching. It swaps
// os.Stdout/os.Stderr for pipes and forwards each line to the original stream
// and to the SSE log broadcaster. Call once, before serving requests.
func (lb *LogBroadcaster) CaptureTerminal() {
	capture := func(target **os.File) {
		orig := *target
		r, w, err := os.Pipe()
		if err != nil {
			return
		}
		*target = w
		go func() {
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for sc.Scan() {
				line := strings.TrimRight(sc.Text(), "\r")
				// Keep the real terminal output identical to before.
				_, _ = orig.WriteString(line + "\n")
				if strings.TrimSpace(line) == "" || logEntryEcho.MatchString(line) {
					continue
				}
				lb.Emit("log", LogEntry{
					Level: "info",
					Msg:   line,
					Time:  time.Now().Format("15:04:05"),
				})
			}
		}()
	}
	capture(&os.Stdout)
	capture(&os.Stderr)
	// The log package cached os.Stderr at init; point it at the captured file.
	log.SetOutput(os.Stderr)
}
