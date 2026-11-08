package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/shengjuntu/rundesk/internal/app"
	"github.com/shengjuntu/rundesk/internal/debugmcp"
)

func runDebugMCP(args []string, in io.Reader, out, errout io.Writer) error {
	flags := flag.NewFlagSet("debug-mcp", flag.ContinueOnError)
	flags.SetOutput(errout)
	origin := flags.String("url", "http://127.0.0.1:3210", "RunDesk origin (HTTPS or loopback HTTP)")
	session := flags.String("session", "", "fixed source session ID (required)")
	env := flags.String("token-env", "RUNDESK_DEBUG_TOKEN", "environment variable containing a scoped read credential")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(*env) {
		return fmt.Errorf("invalid debug-mcp arguments or token environment variable name")
	}
	c, err := debugmcp.New(*origin, *session, os.Getenv(*env))
	if err != nil {
		return err
	}
	return debugmcp.Serve(context.Background(), c, in, out, app.Version)
}
