package app

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

// The server is authoritative. Never invent broader rules or an unadvertised
// session grant. Older servers omit availableDecisions; use conservative defaults.
func approvalDecisions(msg rpc.Message) []any {
	var p map[string]json.RawMessage
	if json.Unmarshal(msg.Params, &p) != nil {
		return []any{}
	}
	if raw, exists := p["availableDecisions"]; exists && string(raw) != "null" {
		var values []any
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return []any{}
		}
		return values
	}
	values := []any{"accept", "decline", "cancel"}
	if msg.Method == "item/commandExecution/requestApproval" {
		var prefix []string
		if json.Unmarshal(p["proposedExecpolicyAmendment"], &prefix) == nil && len(prefix) > 0 {
			values = append(values, map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": prefix}})
		}
	}
	if msg.Method == "item/fileChange/requestApproval" {
		values = append(values, "acceptForSession") // Documented file-change response.
	}
	return values
}

func canonicalJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func approvalResult(msg rpc.Message, decision any, answers map[string]any, content any, scope string) (any, error) {
	choice, _ := decision.(string)
	switch msg.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if decision == nil {
			return nil, errors.New("请选择审批决定")
		}
		for _, allowed := range approvalDecisions(msg) {
			if reflect.DeepEqual(canonicalJSON(decision), canonicalJSON(allowed)) {
				return map[string]any{"decision": allowed}, nil
			}
		}
		return nil, errors.New("该决定不在服务端提供的选项中；请刷新审批，规则内容不能自行扩大")
	case "item/permissions/requestApproval":
		if choice != "accept" && choice != "decline" {
			return nil, errors.New("无效权限决定")
		}
		if scope == "" {
			scope = "turn"
		}
		if scope != "turn" && scope != "session" {
			return nil, errors.New("权限范围必须为 turn 或 session")
		}
		permissions := any(map[string]any{})
		if choice == "accept" {
			var p map[string]any
			if json.Unmarshal(msg.Params, &p) != nil || p["permissions"] == nil {
				return nil, errors.New("缺少请求的权限")
			}
			permissions = p["permissions"]
		} else {
			scope = "turn"
		}
		return map[string]any{"permissions": permissions, "scope": scope}, nil
	case "item/tool/requestUserInput":
		if len(answers) == 0 {
			return nil, errors.New("请填写答案")
		}
		return map[string]any{"answers": answers}, nil
	case "mcpServer/elicitation/request":
		if choice != "accept" && choice != "decline" && choice != "cancel" {
			return nil, errors.New("无效 MCP 决定")
		}
		if choice != "accept" {
			content = nil
		}
		return map[string]any{"action": choice, "content": content}, nil
	default:
		return nil, errors.New("不支持的审批")
	}
}
