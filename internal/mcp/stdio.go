package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

func Stdio(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = time.Second
	}
	kill := ConfigureProcess(cmd)
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			in.Close()
			_ = kill()
			out.Close()
			_ = cmd.Wait()
		})
	}
	c := &Client{close: shutdown}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 65536), Limit)
	c.exchange = func(callCtx context.Context, msg map[string]any) (json.RawMessage, error) {
		type answer struct {
			raw json.RawMessage
			err error
		}
		done := make(chan answer, 1)
		go func() {
			if e := json.NewEncoder(in).Encode(msg); e != nil {
				done <- answer{err: e}
				return
			}
			id, request := msg["id"]
			if !request {
				done <- answer{}
				return
			}
			total := 0
			for scanner.Scan() {
				total += len(scanner.Bytes()) + 1
				if total > Limit {
					done <- answer{err: fmt.Errorf("MCP response exceeds 2 MiB")}
					return
				}
				raw, match, e := Result(scanner.Bytes(), id)
				if reply, ok := serverResponse(e); ok {
					if e = json.NewEncoder(in).Encode(reply); e != nil {
						done <- answer{err: e}
						return
					}
					continue
				}
				if e != nil || match {
					done <- answer{raw, e}
					return
				}
			}
			e := scanner.Err()
			if e == nil {
				e = io.EOF
			}
			done <- answer{err: e}
		}()
		select {
		case v := <-done:
			if v.err != nil {
				shutdown()
			}
			return v.raw, v.err
		case <-callCtx.Done():
			shutdown()
			<-done
			return nil, callCtx.Err()
		case <-ctx.Done():
			shutdown()
			<-done
			return nil, ctx.Err()
		}
	}
	return c, nil
}
