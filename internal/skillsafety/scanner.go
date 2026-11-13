package skillsafety

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The digest intentionally includes the executable permission bit. Two bundles
// with equal source bytes but different execution permissions are not equal.
func Digest(files []File) (string, error) {
	if len(files) == 0 || len(files) > MaxFiles {
		return "", fmt.Errorf("skill must contain 1–%d files", MaxFiles)
	}
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	hash := sha256.New()
	hash.Write([]byte("rundesk-skill-v1\x00"))
	seen, total := map[string]bool{}, 0
	for _, file := range ordered {
		if !safePath(file.Path) || !file.Mode.IsRegular() {
			return "", fmt.Errorf("invalid skill source path or type")
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return "", fmt.Errorf("duplicate or case-conflicting skill path")
		}
		seen[key] = true
		total += len(file.Data)
		if total > MaxTotalBytes {
			return "", fmt.Errorf("skill exceeds %d byte digest limit", MaxTotalBytes)
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(file.Path)))
		hash.Write(length[:])
		hash.Write([]byte(file.Path))
		binary.BigEndian.PutUint64(length[:], uint64(len(file.Data)))
		hash.Write(length[:])
		hash.Write(file.Data)
		if file.Mode&0111 != 0 {
			hash.Write([]byte{1})
		} else {
			hash.Write([]byte{0})
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func safePath(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:\x00\r\n") || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, char := range part {
			if char < 32 {
				return false
			}
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9') {
			return false
		}
	}
	return true
}

var (
	privateKeyRE    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|$)`)
	tokenRE         = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{12,}|hf_[A-Za-z0-9]{12,}|gh[pousr]_[A-Za-z0-9_]{12,}|github_pat_[A-Za-z0-9_]{12,}|AKIA[A-Z0-9]{16})\b`)
	secretValueRE   = regexp.MustCompile(`(?i)(\b(api[_-]?key|secret([_-]?key)?|(access[_-]?)?token|password|passwd|authorization)\b["']?\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;}]+)`)
	bearerRE        = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	urlCredentialRE = regexp.MustCompile(`(://)[^\s/@:]+:[^\s/@]+@`)
	longValueRE     = regexp.MustCompile(`[A-Za-z0-9+/=_-]{80,}`)
)

// Redact is shared by every report-producing engine. It returns a short display
// excerpt, never a raw credential or private-key body.
func Redact(value string) string {
	value = privateKeyRE.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = tokenRE.ReplaceAllString(value, "[REDACTED_TOKEN]")
	value = secretValueRE.ReplaceAllString(value, "${1}[REDACTED]")
	value = bearerRE.ReplaceAllString(value, "Bearer [REDACTED]")
	value = urlCredentialRE.ReplaceAllString(value, "${1}[REDACTED_CREDENTIALS]@")
	value = longValueRE.ReplaceAllString(value, "[REDACTED_LONG_VALUE]")
	runes := []rune(value)
	if len(runes) > 300 {
		return string(runes[:300]) + "…"
	}
	return value
}

var (
	overrideRE        = regexp.MustCompile(`(?i)\b(ignore|disregard|override|bypass)\s+((all|any|the|your)\s+)*((previous|prior|system|developer|user|safety|higher[- ]priority)\s+)+(instructions?|prompts?|messages?|rules?|constraints?)\b|忽略(之前|先前|以上|所有|系统|开发者|用户).{0,16}(指令|要求|规则)|(覆盖|绕过)(系统|开发者|用户|安全).{0,12}(指令|规则|限制)`)
	negatedOverrideRE = regexp.MustCompile(`(?i)\b(never|do not|don't|must not|should not)\s*$|(不要|不得|禁止)\s*$`)
	concealRE         = regexp.MustCompile(`(?i)\b(do not|don't|never)\s+(tell|inform|notify)\s+(the\s+)?user\b|\b(hide|conceal)\b.{0,50}\bfrom\s+(the\s+)?user\b|(不要|不必|无需)(告诉|通知|告知)用户|(向|对)用户隐瞒|静默(执行|上传|发送)|悄悄(执行|上传|发送)`)
	protectSecretRE   = regexp.MustCompile(`(?i)^\s+(passwords?|secrets?|tokens?|private keys?|credentials?)\b`)
	sensitiveRE       = regexp.MustCompile(`(?i)(~[/\\]\.ssh[/\\](id_[\w-]+|config)|\.ssh[/\\]id_[\w-]+|\.aws[/\\]credentials|\.config[/\\][\w/-]*(credentials|tokens?)|(^|[\s@"'/\\])\.env(\b|[.\w-]*)|/etc/shadow|(credentials?|secrets?|tokens?)\.(json|ya?ml|txt)|(api[_-]?keys?|access[_-]?tokens?|passwords?))`)
	readerRE          = regexp.MustCompile(`(?i)\b(cat|type|Get-Content|readFile(Sync)?|read_text|read_bytes|open|copy|cp|scp|read)\b|读取|收集|复制`)
	outboundRE        = regexp.MustCompile(`(?i)\bcurl\b[^\n]*(--data(-binary|-raw|-urlencode)?\b|\s-[dF](\s|@)|--upload-file\b|\s-T\s)|\bwget\b[^\n]*--post-(data|file)\b|\brequests\.(post|put)\s*\(|\b(fetch|axios\.(post|put))\s*\([^\n]*(body|data|https?://)|\bInvoke-(RestMethod|WebRequest)\b[^\n]*-(Body|InFile)\b|\b(scp|sftp)\b[^\n]*@`)
	remoteRE          = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	pipeExecRE        = regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n|;]*https?://[^\n|;]*\|\s*(sudo\s+)?(ba|z|da|k)?sh\b|\b(curl|wget)\b[^\n]*\|\s*(python[23]?|perl|ruby)\b|\b(iwr|irm|Invoke-WebRequest|Invoke-RestMethod)\b[^\n]*\|\s*(iex|Invoke-Expression)\b`)
	downloadRE        = regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n]*(-o\s+|--output\s+|-O\s+|>\s*)["']?([\w./-]+)`)
	deleteRE          = regexp.MustCompile(`(?i)\brm\s+[^\n;|]+|\b(Remove-Item|del|rd|rmdir)\b[^\n]*(-Recurse|/s\b)[^\n]*`)
	recursiveDeleteRE = regexp.MustCompile(`(?i)(-[A-Za-z]*r[A-Za-z]*f|-[A-Za-z]*f[A-Za-z]*r|--recursive)`)
	broadDeleteRE     = regexp.MustCompile(`(?i)(\s|["'])(/\s*$|/\*|~([/\\*\s]|$)|\$\{?HOME\}?|%USERPROFILE%|[A-Za-z]:[\\/](\*|["']?\s*$))`)
	permissionsRE     = regexp.MustCompile(`(?i)\bchmod\s+(-[a-zA-Z]+\s+)*(777|a\+rwx|o\+w)\b[^\n]*|\b(echo|printf|tee)\b[^\n]*(NOPASSWD|/etc/sudoers)|\bSet-ExecutionPolicy\b[^\n]*\b(Bypass|Unrestricted)\b|\b(ufw\s+disable|setenforce\s+0)\b`)
	hostPermissionRE  = regexp.MustCompile(`(?i)sudoers|/etc/|/usr/|/bin/|ufw|setenforce`)
	persistenceRE     = regexp.MustCompile(`(?i)\b(crontab\s+(-|[^\n]+)|schtasks(\.exe)?\s+/create|New-ScheduledTask\b|Register-ScheduledTask\b)|(>|write_text\s*\(|writeFile\s*\(|copy\s+|cp\s+)[^\n]*(/etc/cron|/etc/systemd/system|\.config/systemd/user|LaunchAgents|CurrentVersion[\\/]Run)|\breg\s+add\b[^\n]*CurrentVersion[\\/]Run`)
	harvestRE         = regexp.MustCompile(`(?i)\b(printenv|dict\s*\(\s*os\.environ\s*\)|os\.environ\.(items|keys|values)\s*\(|Get-ChildItem\s+Env:)|\benv\s*\|`)
	encodedRE         = regexp.MustCompile(`(?i)\b(exec|eval)\s*\([^\n]*(b64decode|base64|atob|fromCharCode)|\b(powershell|pwsh)(\.exe)?\b[^\n]*-(EncodedCommand|enc)\s+\S+|\b(base64\s+(-d|--decode)|b64decode\s*\()[^\n]*\|\s*(ba)?sh\b`)
	embeddedSecretRE  = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{20,}|hf_[A-Za-z0-9]{20,}|gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16})\b|-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	traversalRE       = regexp.MustCompile(`(?i)(\]\(\s*|(source|include|path|script|read|open|import)\s*([:=(]|from)?\s*["']?)(\.\.[/\\])+[^\s)"']+`)
	educationRE       = regexp.MustCompile(`(?i)\b(example|examples|quoted|quotation)\b|\b(scanner|detector|rule)\s+(detects|flags|matches|rejects|recognizes)\b|示例|例如|比如|不要遵循|不应遵循|禁止执行|(检测|识别|拒绝).{0,12}(指令|命令|攻击|注入)`)
)

type sourceRule struct {
	id, severity, category, reason, advice string
	match                                  *regexp.Regexp
}

var directRules = []sourceRule{
	{"SG004", "high", "remote_execution", "下载内容被直接交给解释器执行，远端内容可能改变。", "固定来源版本，先下载、校验并审查，再执行。", pipeExecRE},
	{"SG006", "medium", "weakened_permissions", "命令放宽权限、执行限制或主机防护。", "使用最小必要权限并审查变更范围。", permissionsRE},
	{"SG007", "high", "persistence", "内容创建计划任务、开机启动项或持久化服务。", "展示持久化目的、目标及卸载方式。", persistenceRE},
	{"SG009", "medium", "environment_harvesting", "枚举全部环境变量可能收集无关凭据。", "只读取明确需要的变量，避免记录或传输整份环境。", harvestRE},
	{"SG010", "high", "encoded_execution", "编码与动态执行组合使实际内容难以审查。", "展开并审查实际命令，移除不必要的动态执行。", encodedRE},
	{"SG011", "high", "embedded_credential", "发现看起来像访问令牌或私钥的内容，未验证有效性。", "移除凭据；真实泄露应撤销并轮换。", embeddedSecretRE},
	{"SG012", "medium", "escaping_reference", "相对引用包含父目录跳转，可能访问 Skill 根目录之外。", "确认引用解析范围，将必要资源放入受控目录。", traversalRE},
}

func decodeText(data []byte) (string, bool) {
	if len(data) >= 2 && (bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff})) {
		if (len(data)-2)%2 != 0 {
			return "", false
		}
		units := make([]uint16, 0, (len(data)-2)/2)
		for i := 2; i < len(data); i += 2 {
			var unit uint16
			if data[0] == 0xff {
				unit = binary.LittleEndian.Uint16(data[i : i+2])
			} else {
				unit = binary.BigEndian.Uint16(data[i : i+2])
			}
			if unit >= 0xd800 && unit <= 0xdbff {
				if i+3 >= len(data) {
					return "", false
				}
				var next uint16
				if data[0] == 0xff {
					next = binary.LittleEndian.Uint16(data[i+2 : i+4])
				} else {
					next = binary.BigEndian.Uint16(data[i+2 : i+4])
				}
				if next < 0xdc00 || next > 0xdfff {
					return "", false
				}
			} else if unit >= 0xdc00 && unit <= 0xdfff {
				if len(units) == 0 || units[len(units)-1] < 0xd800 || units[len(units)-1] > 0xdbff {
					return "", false
				}
			}
			units = append(units, unit)
		}
		return string(utf16.Decode(units)), true
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	controls := 0
	for _, char := range string(data) {
		if char < 32 && !strings.ContainsRune("\n\r\t\f", char) {
			controls++
		}
	}
	if controls > 2 && controls > len(data)/100 {
		return "", false
	}
	return string(data), true
}

// Scan examines only the supplied bytes. It never opens referenced paths,
// imports code, follows URLs, or executes source. No findings is not a guarantee.
func Scan(files []File) Report {
	report := Report{Engine: "builtin", EngineVersion: Version, Status: "completed", Recommendation: "no_findings", Findings: []Finding{}, Warnings: []string{"静态信号检查不证明安全或恶意；不分析完整跨文件数据流、图片或远程引用内容。"}, Coverage: Coverage{FilesSeen: len(files), Skipped: []Skipped{}}}
	digest, err := Digest(files)
	if err == nil {
		report.Digest = digest
	}
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	seen, total := map[string]bool{}, 0
	skip := func(name, reason string) {
		report.Coverage.Skipped = append(report.Coverage.Skipped, Skipped{File: name, Reason: reason})
	}
	if len(files) == 0 {
		skip(".", "empty_input")
	}
	for index, file := range ordered {
		reason := ""
		key := strings.ToLower(file.Path)
		switch {
		case !safePath(file.Path):
			reason = "unsafe_path"
		case !file.Mode.IsRegular():
			reason = "non_regular_file"
		case seen[key]:
			reason = "duplicate_or_case_conflicting_path"
		case index >= MaxFiles:
			reason = "file_count_limit"
		case len(file.Data) > MaxFileBytes:
			reason = "file_size_limit"
		case total+len(file.Data) > MaxTotalBytes:
			reason = "total_size_limit"
		case len(report.Findings) >= MaxFindings:
			reason = "finding_count_limit"
		}
		seen[key] = true
		if reason != "" {
			skip(file.Path, reason)
			continue
		}
		total += len(file.Data)
		text, ok := decodeText(file.Data)
		if !ok {
			skip(file.Path, "binary_or_unsupported_encoding")
			continue
		}
		truncated := scanText(file.Path, text, &report.Findings)
		report.Coverage.FilesScanned++
		if truncated {
			skip(file.Path, "finding_count_limit_partial_file")
		}
	}
	report.Coverage.FilesSkipped = len(report.Coverage.Skipped)
	if len(report.Coverage.Skipped) > 0 || err != nil {
		report.Status = "incomplete"
		report.Recommendation = "incomplete"
	} else if len(report.Findings) > 0 {
		report.Recommendation = "review"
	}
	return report
}

func scanText(name, text string, findings *[]Finding) bool {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	document := false
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".txt", ".rst":
		document = true
	}
	add := func(rule, severity, category string, line int, evidence, reason, advice string) bool {
		if len(*findings) >= MaxFindings {
			return false
		}
		if document && (category == "instruction_override" || category == "concealed_behavior") && educationRE.MatchString(remoteRE.ReplaceAllString(evidence, "[URL]")) {
			severity = "low"
			reason = "教育性或描述性上下文中的静态信号，尚未确认实际执行。 " + reason
		}
		id := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", rule, name, line, evidence)))
		*findings = append(*findings, Finding{ID: "finding-" + hex.EncodeToString(id[:8]), RuleID: rule, Severity: severity, Category: category, File: name, Line: line, Evidence: Redact(evidence), Reason: reason, Recommendation: advice})
		return true
	}
	for index, line := range lines {
		number := index + 1
		if match := overrideRE.FindStringIndex(line); match != nil {
			prefix := line[:match[0]]
			if !negatedOverrideRE.MatchString(prefix) && !add("SG001", "high", "instruction_override", number, line, "内容要求忽略或覆盖更高优先级或用户指令。", "核对实际意图，移除指令覆盖要求。") {
				return true
			}
		}
		if match := concealRE.FindStringIndex(line); match != nil && !protectSecretRE.MatchString(line[match[1]:]) {
			if !add("SG002", "high", "concealed_behavior", number, line, "内容要求向用户隐瞒信息或静默执行，目的仍需人工判断。", "说明行为、数据去向与授权要求。") {
				return true
			}
		}
		if sensitiveRE.MatchString(line) && readerRE.MatchString(line) {
			if !add("SG003", "high", "credential_access", number, line, "敏感凭据路径或名称与读取/复制操作出现在同一行。", "限定范围并确认凭据访问是否必要。") {
				return true
			}
		}
		for _, rule := range directRules {
			if rule.match.MatchString(line) {
				severity := rule.severity
				if rule.id == "SG006" && hostPermissionRE.MatchString(line) {
					severity = "high"
				}
				if !add(rule.id, severity, rule.category, number, line, rule.reason, rule.advice) {
					return true
				}
			}
		}
		if command := deleteRE.FindString(line); command != "" {
			if !strings.HasPrefix(strings.ToLower(command), "rm ") || recursiveDeleteRE.MatchString(command) {
				severity := "medium"
				if broadDeleteRE.MatchString(command) {
					severity = "critical"
				}
				if !add("SG005", severity, "destructive_operation", number, line, "存在递归删除，静态检查无法确认运行时目标范围。", "限定解析后的目标路径，并核对破坏性操作授权。") {
					return true
				}
			}
		}
		if remoteRE.MatchString(line) && !pipeExecRE.MatchString(line) {
			if match := downloadRE.FindStringSubmatch(line); len(match) > 3 {
				filename := match[3]
				end := index + 5
				if end > len(lines) {
					end = len(lines)
				}
				window := strings.Join(lines[index:end], "\n")
				run := regexp.MustCompile(`(?im)(\b(ba)?sh|\bpython[23]?|\bperl|\bruby)\s+["']?` + regexp.QuoteMeta(filename) + `(["'\s;]|$)|(^|[;\n])\s*` + regexp.QuoteMeta(filename) + `(\s|$)`)
				if run.MatchString(window) && !add("SG004", "high", "remote_execution", number, window, "短窗口内出现下载文件后运行同一路径的组合。", "固定下载摘要，审查下载内容后再运行。") {
					return true
				}
			}
		}
		if outboundRE.MatchString(line) {
			start, end := index-3, index+3
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			window := strings.Join(lines[start:end], "\n")
			if sensitiveRE.MatchString(window) || harvestRE.MatchString(window) {
				severity := "high"
				if sensitiveRE.MatchString(line) {
					severity = "critical"
				}
				if !add("SG008", severity, "possible_exfiltration", number, window, "外发与敏感信息收集信号出现在同一短窗口，尚未证明实际数据流。", "确认发送目标、实际数据流和用户授权。") {
					return true
				}
			}
		}
	}
	return false
}
