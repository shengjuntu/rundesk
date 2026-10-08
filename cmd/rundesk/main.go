package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/shengjuntu/rundesk/internal/app"
	"github.com/shengjuntu/rundesk/internal/containerruntime"
	"github.com/shengjuntu/rundesk/internal/servicelock"
	"github.com/shengjuntu/rundesk/internal/tracequery"
)

func main() {
	if handled, e := containerruntime.Dispatch(os.Args[1:]); handled {
		if e != nil {
			log.Fatal(e)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "__trace_mcp" {
		if len(os.Args) != 5 {
			log.Fatal("trace MCP requires database, source session, snapshot cursor")
		}
		through, e := strconv.ParseInt(os.Args[4], 10, 64)
		if e != nil {
			log.Fatal(e)
		}
		r, e := tracequery.Open(os.Args[2], os.Args[3], through)
		if e != nil {
			log.Fatal(e)
		}
		defer r.Close()
		if e = tracequery.Serve(r, os.Stdin, os.Stdout); e != nil {
			log.Fatal(e)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "__demo_agent" {
		app.DemoAgent()
		return
	}
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	base, e := os.UserConfigDir()
	if e != nil {
		base = "."
	}
	addr := flag.String("listen", "127.0.0.1:3210", "HTTP listen address")
	data := flag.String("data", defaultDataDir(base), "persistent data directory; reuses a v0.1 codex-base database when present")
	codex := flag.String("codex", "codex", "Codex executable")
	demo := flag.Bool("demo", false, "explicit simulation; no model calls, isolated demo data")
	publicURL := flag.String("public-url", "", "exact public origin behind a reverse proxy, e.g. https://codex.example.com")
	version := flag.Bool("version", false, "print RunDesk version")
	flag.Parse()
	if *version {
		fmt.Println("RunDesk " + app.Version)
		return nil
	}
	if *demo {
		*data = filepath.Join(*data, "demo")
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	local := host == "localhost" || (ip != nil && ip.IsLoopback())
	token := os.Getenv("RUNDESK_TOKEN")
	if token == "" {
		token = os.Getenv("CODEX_BASE_TOKEN")
	}
	if (!local || *publicURL != "") && len(token) < 24 {
		return fmt.Errorf("远程监听需要设置至少 24 字符的 RUNDESK_TOKEN")
	}
	if *publicURL != "" {
		u, e := url.Parse(*publicURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("--public-url 必须为没有路径的 HTTPS 来源，例如 https://codex.example.com")
		}
	}
	// A second process must not recover or mutate the same live database.
	if e = os.MkdirAll(*data, 0700); e != nil {
		return e
	}
	lock := filepath.Join(*data, "server.lock")
	guard, e := servicelock.Acquire(lock)
	if e != nil {
		return fmt.Errorf("data directory lock %s: %w", lock, e)
	}
	defer guard.Close()

	m, e := app.New(*data, *codex, *demo)
	if e != nil {
		return e
	}
	defer m.Close()
	server := &http.Server{Addr: *addr, Handler: app.NewHandler(m, token, local, *publicURL), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	listener, e := net.Listen("tcp", *addr)
	if e != nil {
		return e
	}
	errc := make(chan error, 1)
	go func() { errc <- server.Serve(listener) }()
	log.Printf("RunDesk http://%s · demo=%v · data=%s", listener.Addr(), *demo, *data)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtimeFailure := false
	select {
	case <-ctx.Done():
	case <-m.Done():
		runtimeFailure = true
		log.Print("运行管理器停止；请检查日志和磁盘空间")
	case e = <-errc:
		if e != nil && e != http.ErrServerClosed {
			return e
		}
	}
	// Close streaming responses as part of shutdown; native runs are not rerun on restart.
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = server.Shutdown(shutdown); e != nil {
		_ = server.Close()
	}
	if runtimeFailure {
		return fmt.Errorf("runtime manager stopped unexpectedly; inspect the journal before resuming interrupted work")
	}
	return nil
}

// Continue using the old directory in place: workspace paths and Codex thread
// history refer to absolute paths, so silently moving them would break upgrades.
func defaultDataDir(base string) string {
	current := filepath.Join(base, "rundesk")
	old := filepath.Join(base, "codex-base")
	exists := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "state.db"))
		if err == nil {
			return true
		}
		_, err = os.Stat(filepath.Join(dir, "demo", "state.db"))
		return err == nil
	}
	if !exists(current) && exists(old) {
		return old
	}
	return current
}
