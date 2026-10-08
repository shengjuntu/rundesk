package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	k "github.com/shengjuntu/rundesk/internal/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/servicelock"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	data := flag.String("data", "", "session execution directory")
	version := flag.Bool("version", false, "print version")
	flag.Parse()
	if *version {
		fmt.Println("Kun " + p.EngineVersion)
		return
	}
	if *data == "" {
		log.Fatal("--data is required")
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	lock, err := servicelock.Acquire(filepath.Join(*data, "worker.lock"))
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()
	engine, err := k.Open(filepath.Join(*data, "state.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()
	served := make(chan error, 1)
	go func() { served <- serve(engine, os.Stdin, os.Stdout) }()
	select {
	case err = <-served:
	case err = <-engine.Fatal():
	}
	if err != nil {
		log.Print(err)
	}
}
func serve(e *k.Engine, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 65536), p.MaxMessage)
	enc := json.NewEncoder(output)
	for scanner.Scan() {
		var in p.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &in); err != nil {
			return fmt.Errorf("invalid protocol JSON: %w", err)
		}
		out := p.Envelope{Version: p.Version, ID: in.ID}
		var value any
		var err error
		if in.Version != p.Version {
			err = fmt.Errorf("unsupported protocol version %d", in.Version)
		} else {
			switch in.Method {
			case "hello":
				value = map[string]any{"engine": "kun", "engineVersion": p.EngineVersion, "protocolVersion": p.Version, "pid": os.Getpid(), "capabilities": map[string]bool{"inspect": true, "pause": true, "step": true, "steer": true, "replayEvents": true, "resumeCheckpoint": true, "fork": false, "mcp": true, "mcpApproval": true, "modules": true, "budgets": true, "argumentValidation": true, "conditionalBreakpoints": true, "debugQueries": true}}
			case "start":
				var v p.Start
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.Start(v)
				}
			case "checkpoint":
				var v p.Start
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.CheckpointFor(v)
				}
			case "query":
				var v p.DebugQuery
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.Query(v)
				}
			case "state":
				value = e.State()
			case "control":
				var v p.Control
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.Control(v)
				}
			case "events":
				var v struct {
					After int64 `json:"after"`
				}
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.Events(v.After)
				}
			case "snapshot":
				var v struct {
					Sequence int64 `json:"sequence"`
				}
				err = json.Unmarshal(in.Params, &v)
				if err == nil {
					value, err = e.Snapshot(v.Sequence)
				}
			default:
				err = fmt.Errorf("unsupported method %s", strconv.Quote(in.Method))
			}
		}
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Result = p.JSON(value)
		}
		if err = enc.Encode(out); err != nil {
			return err
		}
	}
	return scanner.Err()
}
