package app

// DemoAgent is an explicitly selected protocol fixture, not a substitute model.
import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

func DemoAgent() {
	var outMu sync.Mutex
	send := func(m any) { outMu.Lock(); defer outMu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(m) }
	notify := func(method string, p any) { send(map[string]any{"method": method, "params": p}) }
	var mu sync.Mutex
	pending := map[string]chan json.RawMessage{}
	var cancel chan struct{}
	var activeTurn string
	thread, cwd := "", ""
	servers := map[string]any{}
	version := 0
	disabledSkills := map[string]bool{}
	statePath := filepath.Join(os.Getenv("CODEX_HOME"), "rundesk-demo.json")
	type configState struct {
		Servers  map[string]any
		Version  int
		Disabled map[string]bool
	}
	load := func() {
		var state configState
		if b, e := os.ReadFile(statePath); e == nil && json.Unmarshal(b, &state) == nil {
			servers, version, disabledSkills = state.Servers, state.Version, state.Disabled
		}
	}
	save := func() {
		b, _ := json.Marshal(configState{servers, version, disabledSkills})
		temp := statePath + "." + store.ID()
		if os.WriteFile(temp, b, 0600) == nil {
			_ = os.Rename(temp, statePath)
		}
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 65536), 4<<20)
	for sc.Scan() {
		var msg rpc.Message
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		if msg.Method == "" {
			mu.Lock()
			ch := pending[string(msg.ID)]
			delete(pending, string(msg.ID))
			mu.Unlock()
			if ch != nil {
				ch <- msg.Result
			}
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(msg.Params, &p)
		reply := func(v any) { send(map[string]any{"id": msg.ID, "result": v}) }
		load()
		switch msg.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "rundesk-demo/0.6.1"})
		case "initialized":
		case "thread/start", "thread/resume":
			thread, _ = p["threadId"].(string)
			if thread == "" {
				thread = "demo-" + store.ID()
			}
			cwd, _ = p["cwd"].(string)
			mode, _ := p["sandbox"].(string)
			if mode == "" {
				mode = "workspace-write"
			}
			network := false
			if c, ok := p["config"].(map[string]any); ok {
				network, _ = c["sandbox_workspace_write.network_access"].(bool)
			}
			model, _ := p["model"].(string)
			if model == "" {
				model = "demo-fixture"
			}
			reply(map[string]any{"thread": map[string]any{"id": thread, "turns": []any{}}, "model": model, "modelProvider": "demo", "cwd": cwd, "approvalPolicy": p["approvalPolicy"], "approvalsReviewer": p["approvalsReviewer"], "sandbox": map[string]any{"type": mode, "networkAccess": network}})
		case "account/read":
			reply(map[string]any{"account": map[string]string{"type": "demo"}, "requiresOpenaiAuth": false})
		case "model/list":
			reply(map[string]any{"data": []any{map[string]any{"id": "demo-fixture", "model": "demo-fixture", "displayName": "Demo · 协议模拟", "isDefault": true}}, "nextCursor": nil})
		case "skills/list":
			data := []any{}
			dirs, _ := p["cwds"].([]any)
			for _, d := range dirs {
				path, _ := d.(string)
				skills := []any{}
				for _, source := range []struct{ dir, scope string }{{filepath.Join(path, ".agents", "skills"), "repo"}, {filepath.Join(os.Getenv("CODEX_HOME"), "skills"), "user"}} {
					entries, _ := os.ReadDir(source.dir)
					for _, entry := range entries {
						sp := filepath.Join(source.dir, entry.Name(), "SKILL.md")
						if !entry.IsDir() {
							continue
						}
						if _, e := os.Stat(sp); e != nil {
							continue
						}
						skills = append(skills, map[string]any{"name": entry.Name(), "description": "演示模式读取的 Skill", "path": sp, "enabled": !disabledSkills[sp], "scope": source.scope})
					}
				}
				data = append(data, map[string]any{"cwd": path, "skills": skills, "errors": []any{}})
			}
			reply(map[string]any{"data": data})
		case "skills/config/write":
			path, _ := p["path"].(string)
			enabled, _ := p["enabled"].(bool)
			disabledSkills[path] = !enabled
			save()
			reply(map[string]any{"effectiveEnabled": p["enabled"]})
		case "config/read":
			reply(map[string]any{"config": map[string]any{"model": "demo-fixture", "mcp_servers": servers}, "layers": []any{map[string]any{"name": map[string]string{"type": "user", "file": "demo-config.toml"}, "version": fmt.Sprint(version), "config": map[string]any{"mcp_servers": servers}}}, "origins": map[string]any{}})
		case "config/value/write":
			v, _ := p["value"].(map[string]any)
			servers = v
			version++
			save()
			reply(map[string]any{"status": "ok", "version": fmt.Sprint(version), "filePath": "demo-config.toml"})
		case "config/mcpServer/reload":
			reply(map[string]any{})
		case "mcpServerStatus/list":
			reply(map[string]any{"data": []any{}, "nextCursor": nil})
		case "turn/steer":
			mu.Lock()
			target := activeTurn
			if target == "" || p["expectedTurnId"] != target || cancel == nil {
				mu.Unlock()
				send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32602, "message": "No matching active turn"}})
				continue
			}
			reply(map[string]any{"turnId": target})
			notify("item/completed", map[string]any{"threadId": thread, "turnId": target, "item": map[string]any{"id": "steer-" + store.ID(), "type": "userMessage", "content": p["input"]}})
			mu.Unlock()
		case "turn/interrupt":
			mu.Lock()
			if cancel != nil {
				close(cancel)
				cancel = nil
			}
			mu.Unlock()
			reply(map[string]any{})
		case "turn/start":
			turn := "turn-" + store.ID()
			ch := make(chan struct{})
			mu.Lock()
			cancel = ch
			activeTurn = turn
			mu.Unlock()
			tid, dir := thread, cwd
			reply(map[string]any{"turn": map[string]any{"id": turn, "status": "inProgress", "items": []any{}}})
			go func(params map[string]any) {
				base := func(extra map[string]any) map[string]any {
					extra["threadId"] = tid
					extra["turnId"] = turn
					return extra
				}
				notify("turn/started", base(map[string]any{"turn": map[string]any{"id": turn, "status": "inProgress"}}))
				interrupted := false
				defer func() {
					mu.Lock()
					if activeTurn == turn {
						activeTurn = ""
						cancel = nil
					}
					mu.Unlock()
					status := "completed"
					if interrupted {
						status = "interrupted"
					}
					notify("turn/completed", base(map[string]any{"turn": map[string]any{"id": turn, "status": status, "error": nil}}))
				}()
				text := ""
				if arr, ok := params["input"].([]any); ok {
					for _, v := range arr {
						obj, _ := v.(map[string]any)
						if obj["type"] == "text" {
							text, _ = obj["text"].(string)
						}
					}
				}
				userText := strings.SplitN(text, "\n\n[Application context]", 2)[0]
				if strings.Contains(userText, "轨迹") {
					samples := []map[string]any{
						{"id": "reason-" + turn, "type": "reasoning", "summary": []string{"演示：检查输入和项目配置。"}},
						{"id": "inspect-" + turn, "type": "commandExecution", "command": "python checks.py", "cwd": dir, "aggregatedOutput": "演示检查：缺少配置项 MODEL_ENDPOINT（未执行真实命令）。", "exitCode": 1, "status": "failed"},
						{"id": "mcp-" + turn, "type": "mcpToolCall", "server": "demo-inventory", "tool": "list_resources", "arguments": map[string]string{"scope": "workspace"}, "result": map[string]any{"resources": []string{"images", "reports"}, "note": "模拟结果，未调用 MCP"}, "status": "completed"},
						{"id": "compact-" + turn, "type": "contextCompaction"},
					}
					for _, item := range samples {
						startItem := map[string]any{"id": item["id"], "type": item["type"], "command": item["command"], "server": item["server"], "tool": item["tool"]}
						notify("item/started", base(map[string]any{"startedAtMs": time.Now().UnixMilli(), "item": startItem}))
						select {
						case <-ch:
							interrupted = true
							return
						case <-time.After(240 * time.Millisecond):
						}
						notify("item/completed", base(map[string]any{"completedAtMs": time.Now().UnixMilli(), "item": item}))
					}
				}
				if strings.Contains(userText, "告警") {
					for n := 0; n < 2; n++ {
						notify("warning", base(map[string]any{"message": "演示告警：测试重复告警合并，不代表真实模型故障。"}))
					}
				}
				if strings.Contains(userText, "审批") || strings.Contains(strings.ToLower(userText), "approval") {
					id := json.RawMessage(`"approval-demo-` + turn + `"`)
					answer := make(chan json.RawMessage, 1)
					mu.Lock()
					pending[string(id)] = answer
					mu.Unlock()
					defer func() { mu.Lock(); delete(pending, string(id)); mu.Unlock() }()
					send(map[string]any{"id": id, "method": "item/commandExecution/requestApproval", "params": base(map[string]any{"itemId": "command-" + turn, "kind": "command", "command": "echo 'demo: no real command is executed'", "cwd": dir, "reason": "演示审批流程，不执行命令", "startedAtMs": time.Now().UnixMilli(), "availableDecisions": []any{"accept", map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"echo", "demo"}}}, "cancel"}})})
					select {
					case ans := <-answer:
						var d map[string]any
						_ = json.Unmarshal(ans, &d)
						notify("serverRequest/resolved", base(map[string]any{"requestId": stringID(id)}))
						accepted := d["decision"] == "accept" || d["decision"] == "acceptForSession"
						if v, ok := d["decision"].(map[string]any); ok && v["acceptWithExecpolicyAmendment"] != nil {
							accepted = true
						}
						if !accepted {
							interrupted = true
							return
						}
					case <-ch:
						interrupted = true
						return
					}
				}
				itemID := "message-" + turn
				notify("item/started", base(map[string]any{"item": map[string]any{"id": itemID, "type": "agentMessage", "text": ""}}))
				message := "这是 **RunDesk 演示模式**，没有调用真实模型。\n\n运行后台、事件流和调试面板已经连通。你可以刷新页面，验证会话恢复；输入「审批」查看人工审批流程。\n\n真实运行时，后台会直接连接本机的 Codex App Server，继续使用 Codex 的 Skills、MCP 和原生上下文管理。"
				for _, part := range []rune(message) {
					select {
					case <-ch:
						interrupted = true
						return
					case <-time.After(12 * time.Millisecond):
						notify("item/agentMessage/delta", base(map[string]any{"itemId": itemID, "delta": string(part)}))
					}
				}
				notify("item/completed", base(map[string]any{"item": map[string]any{"id": itemID, "type": "agentMessage", "text": message}}))
				notify("thread/tokenUsage/updated", base(map[string]any{"tokenUsage": map[string]any{"total": map[string]int{"totalTokens": 230, "inputTokens": 100, "outputTokens": 130}, "last": map[string]int{"totalTokens": 230, "inputTokens": 100, "outputTokens": 130}, "modelContextWindow": 128000}}))
				// Write through a rooted handle, just like the application's own file API.
				marker := "Save deliverable files in "
				if i := strings.Index(text, marker); i >= 0 {
					out := strings.SplitN(text[i+len(marker):], ". Uploaded files", 2)[0]
					if rel, e := filepath.Rel(dir, out); e == nil && safePath(filepath.ToSlash(rel)) {
						if root, e := os.OpenRoot(dir); e == nil {
							_ = root.MkdirAll(rel, 0700)
							if f, e := root.OpenFile(filepath.Join(rel, "demo-report.md"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); e == nil {
								_, _ = f.WriteString("# RunDesk 演示产物\n\n此文件由协议模拟器生成，用于验证文件展示与下载。\n")
								f.Close()
							}
							root.Close()
						}
					}
				}
			}(p)
		default:
			send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "demo fixture does not implement " + msg.Method}})
		}
	}
}

func stringID(raw json.RawMessage) any { var id any; _ = json.Unmarshal(raw, &id); return id }
