package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: hyprland-computer-use setup [--build-only] | version | serve | stop | console | keyboard | mcp | share [--client ID] [--seconds 300] [--view-only] | ui '{\"op\":\"state\"}'")
	}
	// Version and setup must also work outside a desktop session.
	if os.Args[1] == "version" || os.Args[1] == "--version" {
		if len(os.Args) != 2 {
			return errors.New("version accepts no arguments")
		}
		printVersion(os.Stdout)
		return nil
	}
	// Setup can build offline, without a running desktop or runtime socket.
	if os.Args[1] == "setup" {
		return runSetup(os.Args[2:])
	}
	if os.Args[1] == "console" {
		return runConsole(os.Args[2:])
	}
	if os.Args[1] == "keyboard" {
		return runKeyboard(os.Args[2:])
	}
	dir, e := runtimeDir()
	if e != nil {
		return e
	}
	switch os.Args[1] {
	case "stop":
		f := flag.NewFlagSet("stop", flag.ContinueOnError)
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		if f.NArg() != 0 {
			return errors.New("stop accepts no arguments")
		}
		p, err := findBroker(dir)
		if err != nil {
			return err
		}
		if p == nil {
			fmt.Fprintln(os.Stderr, "No broker is running.")
			return nil
		}
		defer p.close()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return p.stop(ctx)
	case "mcp":
		f := flag.NewFlagSet("mcp", flag.ContinueOnError)
		socket := f.String("socket", filepath.Join(dir, "mcp.sock"), "broker MCP socket")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		c, e := net.Dial("unix", *socket)
		if e != nil {
			return brokerConnectionError(*socket, e)
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
	case "share":
		f := flag.NewFlagSet("share", flag.ContinueOnError)
		client := f.String("client", "", "recipient MCP connection ID; picker asks when multiple are connected")
		seconds := f.Int("seconds", 300, "grant duration, 1–3600 seconds")
		viewOnly := f.Bool("view-only", false, "share pixels only instead of viewing and control")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		if f.NArg() != 0 {
			return errors.New("unexpected share arguments")
		}
		cap := "control"
		if *viewOnly {
			cap = "observe"
		}
		c, e := net.Dial("unix", filepath.Join(dir, "ui.sock"))
		if e != nil {
			return e
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		if e = json.NewEncoder(c).Encode(map[string]any{"op": "begin_share", "client": *client, "seconds": *seconds, "capability": cap}); e != nil {
			return e
		}
		var state UIState
		if e = json.NewDecoder(c).Decode(&state); e != nil {
			return e
		}
		if state.Error != "" {
			return errors.New(state.Error)
		}
		fmt.Fprintln(os.Stdout, "Click a window in the local picker to share it. Escape cancels; no grant exists until selection.")
		return nil
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
		httpAddr := f.String("http", "", "optional Streamable HTTP listen address, e.g. 127.0.0.1:8099")
		publicURL := f.String("public-url", "", "HTTP public origin; HTTPS required for OAuth")
		oauth := f.Bool("oauth", false, "enable built-in OAuth provider with local desktop approval (requires --http)")
		cert := f.String("tls-cert", "", "PEM certificate for direct HTTPS")
		key := f.String("tls-key", "", "PEM private key for direct HTTPS")
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, _ := os.UserHomeDir()
			stateHome = filepath.Join(home, ".local/state")
		}
		data := f.String("data", filepath.Join(stateHome, "computer-use"), "private recordings and audit directory")
		if e = f.Parse(os.Args[2:]); e != nil {
			return e
		}
		readyFile, e := brokerReadyFile()
		if e != nil {
			return e
		}
		if readyFile != nil {
			defer readyFile.Close()
		}
		brokerLock, e := lifecycleLock(filepath.Join(dir, "broker.lock"))
		if e != nil {
			return e
		}
		defer brokerLock.Close()
		if *httpAddr == "" && (*oauth || *publicURL != "" || *cert != "" || *key != "") {
			return errors.New("HTTP/OAuth options require --http")
		}
		if *httpAddr != "" {
			if _, e = httpOrigin(*httpAddr, *publicURL, *oauth, *cert, *key); e != nil {
				return e
			}
		}
		if e = os.MkdirAll(*data, 0700); e != nil {
			return e
		}
		if e = os.Chmod(*data, 0700); e != nil {
			return e
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		d := &Desktop{Dir: dir, Data: *data}
		if e = d.guard(ctx, map[string]any{"op": "status"}); e != nil {
			return guardStartupError(e)
		}
		var devices *waylandDevices
		if d.automaticFallback.Load() {
			devices, e = startAutomaticDevices(ctx, dir)
			fmt.Fprintln(os.Stderr, "Automatic input: independent seat preferred; guarded focus borrowing available for unsupported clients.")
		} else if !d.independentSeat.Load() {
			devices, e = startWaylandDevices(ctx)
		} else {
			devices, e = startWaylandWatcher(ctx)
			fmt.Fprintln(os.Stderr, "Independent-seat input enabled; no native-seat virtual devices created.")
		}
		if e != nil {
			return fmt.Errorf("Wayland connection initialization failed: %w", e)
		}
		defer devices.Close()
		if d.independentSeat.Load() {
			if e = devices.requireGuardPeer(dir); e != nil {
				return e
			}
		}
		deviceDone := devices.done
		ui, e := listenUnix(filepath.Join(dir, "ui.sock"))
		if e != nil {
			return e
		}
		defer ui.Close()
		mcp, e := listenUnix(filepath.Join(dir, "mcp.sock"))
		if e != nil {
			return e
		}
		defer mcp.Close()
		if e = d.guard(ctx, map[string]any{"op": "clear"}); e != nil {
			return e
		}
		b := newBroker(d)
		b.log, e = os.OpenFile(filepath.Join(*data, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer b.log.Close()
		defer b.clear()
		if *httpAddr != "" {
			server, err := startHTTP(ctx, b, *httpAddr, *publicURL, *oauth, *cert, *key, *data)
			if err != nil {
				return err
			}
			defer server.Close()
			fmt.Fprintln(os.Stderr, "HTTP MCP:", *httpAddr, "OAuth:", *oauth)
		}
		go b.maintenance(ctx)
		trayDone := make(chan struct{})
		go func() { defer close(trayDone); b.runTray(ctx) }()
		defer func() { cancel(); <-trayDone }()
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
		if readyFile != nil {
			if _, e = io.WriteString(readyFile, "ready\n"); e != nil {
				return e
			}
			_ = readyFile.Close()
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-deviceDone:
			if ctx.Err() != nil {
				return nil
			}
			cancel()
			return fmt.Errorf("Wayland connection lost; stopping broker: %w", err)
		}
	default:
		return errors.New("unknown command")
	}
}

// Main runs the command-line application.
func Main() {
	if e := run(); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
