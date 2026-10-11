// Command acp-smoke is a developer tool: it spawns `opencode acp`, runs an
// initialize -> session/new (-> session/prompt) sequence and prints the
// streamed updates. Use -dump to capture the raw JSONL protocol.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"oc-discord/internal/acp"
	"oc-discord/internal/agent"
	"oc-discord/internal/launcher"
	"oc-discord/internal/logx"
	"oc-discord/internal/perms"
)

func main() {
	wd, _ := os.Getwd()
	cwd := flag.String("cwd", wd, "working directory for the session")
	prompt := flag.String("prompt", "", "prompt to send (omit to only create the session)")
	bin := flag.String("bin", "", "opencode binary (default: auto-resolve)")
	dump := flag.String("dump", "", "write raw JSONL protocol to this file")
	allow := flag.Bool("allow", false, "auto-allow permission requests")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall timeout")
	flag.Parse()

	log := logx.New("info", "")

	var dumpFile *os.File
	if *dump != "" {
		f, err := os.Create(*dump)
		if err != nil {
			fmt.Fprintln(os.Stderr, "dump:", err)
			os.Exit(1)
		}
		dumpFile = f
		defer f.Close()
	}

	abs, err := filepath.Abs(*cwd)
	if err != nil {
		abs = *cwd
	}
	spec := launcher.Resolve(*bin)
	args := append([]string{}, spec.Args...)
	args = append(args, "acp")
	fmt.Printf("launcher: %s -> %s %v\n", spec.Via, spec.Command, args)

	ag := acp.NewAgent(acp.Options{
		Command:  spec.Command,
		Args:     args,
		Cwd:      abs,
		Env:      os.Environ(),
		Logger:   log,
		OnStderr: func(line string) { fmt.Fprint(os.Stderr, line) },
		RawTap: func(dir string, line []byte) {
			if dumpFile != nil {
				fmt.Fprintf(dumpFile, "%s %s\n", dir, line)
			}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := ag.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	defer ag.Stop()

	meta, err := ag.NewSession(ctx, abs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "session/new:", err)
		os.Exit(1)
	}
	fmt.Printf("sessionId=%s cwd=%s\n", meta.ID, abs)
	for _, co := range meta.Config {
		fmt.Printf("  config %s (%s) type=%s current=%s options=%d\n", co.ID, co.Name, co.Type, co.CurrentString(), len(co.Options))
	}

	if *prompt == "" {
		return
	}

	s := &smokeSink{}
	perm := func(req agent.PermissionRequest) agent.Outcome {
		fmt.Printf("\n[permission] %s (%s) options=%v\n", req.Title, req.Kind, req.Options)
		wanted := "deny"
		if *allow {
			wanted = "allow"
		}
		var opts []perms.Option
		for _, o := range req.Options {
			opts = append(opts, perms.Option{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
		}
		if id, ok := perms.PickOptionID(opts, wanted); ok {
			return agent.Outcome{Selected: true, OptionID: id}
		}
		return agent.Outcome{}
	}

	stop, err := ag.Prompt(ctx, meta.ID, *prompt, s, perm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nprompt:", err)
		os.Exit(1)
	}
	fmt.Printf("\n[stopReason=%s]\n", stop)
}

// smokeSink prints streamed updates to stdout.
type smokeSink struct{}

func (smokeSink) Text(t string) { fmt.Print(t) }
func (smokeSink) Tool(title, status string) {
	fmt.Printf("\n[tool %s] %s\n", status, title)
}
func (smokeSink) Mode(modeID string) { fmt.Printf("\n[mode] %s\n", modeID) }
