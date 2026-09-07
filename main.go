package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: computer-use serve | mcp | ui '{\"op\":\"state\"}'")
	}
	dir, e := runtimeDir()
	if e != nil {
		return e
	}
	switch os.Args[1] {
	case "mcp":
		f := flag.NewFlagSet("mcp", flag.ContinueOnError)
		socket := f.String("socket", filepath.Join(dir, "mcp.sock"), "broker MCP socket")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		c, e := net.Dial("unix", *socket)
		if e != nil {
			return e
		}
		defer c.Close()
		go func() {
			_, _ = io.Copy(c, os.Stdin)
			if u, ok := c.(*net.UnixConn); ok {
				_ = u.CloseWrite()
			}
		}()
		_, e = io.Copy(os.Stdout, c)
		return e
	case "ui":
		if len(os.Args) != 3 {
			return errors.New("ui requires one JSON command")
		}
		var q any
		if e = json.Unmarshal([]byte(os.Args[2]), &q); e != nil {
			return e
		}
		c, e := net.Dial("unix", filepath.Join(dir, "ui.sock"))
		if e != nil {
			return e
		}
		defer c.Close()
		if e = json.NewEncoder(c).Encode(q); e != nil {
			return e
		}
		line, e := readLine(c)
		if e == nil {
			_, e = os.Stdout.Write(line)
		}
		return e
	case "serve":
		f := flag.NewFlagSet("serve", flag.ContinueOnError)
		exe, _ := os.Executable()
		keyboard := f.String("keyboard", filepath.Join(filepath.Dir(exe), "computer-use-keyboard"), "persistent US-layout keyboard helper")
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, _ := os.UserHomeDir()
			stateHome = filepath.Join(home, ".local/state")
		}
		data := f.String("data", filepath.Join(stateHome, "computer-use"), "private recordings and audit directory")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		if e = os.MkdirAll(*data, 0700); e != nil {
			return e
		}
		if e = os.Chmod(*data, 0700); e != nil {
			return e
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		d := &Desktop{dir, *data}
		if e = d.guard(ctx, map[string]any{"op": "status"}); e != nil {
			return e
		}
		helper := exec.CommandContext(ctx, *keyboard)
		helper.Stderr = os.Stderr
		if e = helper.Start(); e != nil {
			return e
		}
		defer func() { _ = helper.Process.Kill(); _ = helper.Wait() }()
		ui, e := listenUnix(filepath.Join(dir, "ui.sock"))
		if e != nil {
			return e
		}
		defer ui.Close()
		defer os.Remove(filepath.Join(dir, "ui.sock"))
		mcp, e := listenUnix(filepath.Join(dir, "mcp.sock"))
		if e != nil {
			return e
		}
		defer mcp.Close()
		defer os.Remove(filepath.Join(dir, "mcp.sock"))
		if e = d.guard(ctx, map[string]any{"op": "clear"}); e != nil {
			return e
		}
		b := newBroker(d)
		b.log, e = os.OpenFile(filepath.Join(*data, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer b.log.Close()
		defer func() { b.mu.Lock(); b.clearLocked(); b.mu.Unlock() }()
		go b.maintenance(ctx)
		go b.runTray(ctx)
		go func() {
			for {
				c, e := ui.Accept()
				if e != nil {
					return
				}
				go b.handleUI(c)
			}
		}()
		go func() {
			for {
				c, e := mcp.Accept()
				if e != nil {
					return
				}
				go b.serveMCP(ctx, c)
			}
		}()
		fmt.Fprintln(os.Stderr, "Computer Use ready; approve mode. MCP:", filepath.Join(dir, "mcp.sock"))
		<-ctx.Done()
		return nil
	default:
		return errors.New("unknown command")
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
