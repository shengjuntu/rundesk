package skillsafety

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const spectorStdoutLimit = 8 << 20
const spectorStderrLimit = 64 << 10

// ProbeResult describes installation, not a completed scan or model readiness.
type ProbeResult struct {
	Engine    string `json:"engine"`
	Installed bool   `json:"installed"`
	Message   string `json:"message"`
}

// Probe only resolves a local executable. It does not install, run, or log it.
func Probe(executable string) ProbeResult {
	if strings.TrimSpace(executable) == "" {
		executable = "skillspector"
	}
	_, err := exec.LookPath(executable)
	if err != nil {
		return ProbeResult{"skillspector", false, "SkillSpector 未安装或配置的程序不可用"}
	}
	return ProbeResult{"skillspector", true, "已找到程序；具体运行能力以扫描结果为准"}
}

type spectorError struct {
	code    string
	message string
}

func (e *spectorError) Error() string { return e.message }

// cappedOutput drains the child's pipe while retaining only a bounded prefix.
// Returning len(p) avoids changing a scanner's semantics through a broken pipe.
type cappedOutput struct {
	mu       sync.Mutex
	bytes    []byte
	limit    int
	exceeded bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.bytes)
	if remaining < len(p) {
		b.exceeded = true
	}
	if remaining > 0 {
		count := len(p)
		if count > remaining {
			count = remaining
		}
		b.bytes = append(b.bytes, p[:count]...)
	}
	return len(p), nil
}

func (b *cappedOutput) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.bytes...), b.exceeded
}

type spectorReport struct {
	ExecutionSuccessful *bool `json:"execution_successful"`
	RiskAssessment      struct {
		Recommendation string `json:"recommendation"`
		Severity       string `json:"severity"`
	} `json:"risk_assessment"`
	Completeness struct {
		Complete            *bool             `json:"is_complete"`
		Status              string            `json:"status"`
		Coverage            *float64          `json:"coverage_percent"`
		FilesSeen           *int              `json:"files_seen"`
		FilesScanned        *int              `json:"files_scanned"`
		FilesSkipped        *int              `json:"files_skipped"`
		FullyInspected      *int              `json:"fully_inspected_files"`
		PartiallyInspected  *int              `json:"partially_inspected_files"`
		EntirelyUninspected *int              `json:"entirely_uninspected_files"`
		LedgerExceptions    []spectorScopeGap `json:"ledger_exceptions"`
		ScopeExclusions     []spectorScopeGap `json:"scope_exclusions"`
	} `json:"analysis_completeness"`
	Metadata struct {
		Version      string `json:"skillspector_version"`
		LLMRequested *bool  `json:"llm_requested"`
		LLMAvailable *bool  `json:"llm_available"`
		LLMError     string `json:"llm_error"`
	} `json:"metadata"`
	Components []struct {
		Path string `json:"path"`
	} `json:"components"`
	Issues []struct {
		ID             string          `json:"id"`
		FindingID      string          `json:"finding_id"`
		RuleID         string          `json:"rule_id"`
		Severity       string          `json:"severity"`
		Category       string          `json:"category"`
		Message        string          `json:"message"`
		Description    string          `json:"description"`
		Explanation    string          `json:"explanation"`
		Finding        string          `json:"finding"`
		Evidence       json.RawMessage `json:"evidence"`
		CodeSnippet    string          `json:"code_snippet"`
		Title          string          `json:"title"`
		FilePath       string          `json:"file_path"`
		LineNumber     int             `json:"line_number"`
		Recommendation string          `json:"recommendation"`
		Remediation    string          `json:"remediation"`
		Location       struct {
			File      string `json:"file"`
			StartLine int    `json:"start_line"`
			Line      int    `json:"line"`
		} `json:"location"`
	} `json:"issues"`
}

type spectorScopeGap struct {
	Path       string `json:"path"`
	ReasonCode string `json:"reason_code"`
	Message    string `json:"message"`
}

func emptySpectorReport() Report {
	return Report{
		Engine: "skillspector", Status: "error", Recommendation: "incomplete",
		Findings: []Finding{}, Coverage: Coverage{Skipped: []Skipped{}}, Warnings: []string{},
	}
}

func spectorReportError(report Report, code, message string) (Report, error) {
	report.Status = "error"
	report.Recommendation = "incomplete"
	report.Warnings = append(report.Warnings, message)
	return report, &spectorError{code, message}
}

// parseSpectorReport intentionally requires explicit completeness and execution
// facts. Older reports without those fields cannot be promoted to complete.
func parseSpectorReport(data []byte, exitCode int, allowLLM bool) (Report, error) {
	report := emptySpectorReport()
	var source spectorReport
	if err := json.Unmarshal(data, &source); err != nil {
		return spectorReportError(report, "invalid_report", "SkillSpector 未返回有效的 JSON 报告")
	}
	report.EngineVersion = Redact(source.Metadata.Version)
	ledgerConsistent := true
	for _, count := range []*int{source.Completeness.FilesSeen, source.Completeness.FilesScanned, source.Completeness.FilesSkipped, source.Completeness.FullyInspected, source.Completeness.PartiallyInspected, source.Completeness.EntirelyUninspected} {
		if count != nil && (*count < 0 || *count > MaxFiles) {
			ledgerConsistent = false
		}
	}
	report.Coverage.FilesSeen = len(source.Components)
	if source.Completeness.FilesSeen != nil && *source.Completeness.FilesSeen >= 0 {
		report.Coverage.FilesSeen = *source.Completeness.FilesSeen
	}
	if source.Completeness.FilesScanned != nil && *source.Completeness.FilesScanned >= 0 {
		report.Coverage.FilesScanned = *source.Completeness.FilesScanned
	} else if source.Completeness.FullyInspected != nil && *source.Completeness.FullyInspected >= 0 {
		report.Coverage.FilesScanned = *source.Completeness.FullyInspected
	}
	if source.Completeness.FilesSkipped != nil && *source.Completeness.FilesSkipped >= 0 {
		report.Coverage.FilesSkipped = *source.Completeness.FilesSkipped
	} else {
		if value := source.Completeness.PartiallyInspected; value != nil && *value >= 0 {
			report.Coverage.FilesSkipped += *value
		}
		if value := source.Completeness.EntirelyUninspected; value != nil && *value >= 0 {
			report.Coverage.FilesSkipped += *value
		}
	}
	if report.Coverage.FilesScanned+report.Coverage.FilesSkipped > report.Coverage.FilesSeen {
		if source.Completeness.FilesSeen != nil {
			ledgerConsistent = false
		}
		report.Coverage.FilesSeen = report.Coverage.FilesScanned + report.Coverage.FilesSkipped
	}
	if seen, scanned := source.Completeness.FilesScanned, source.Completeness.FullyInspected; seen != nil && scanned != nil && *seen != *scanned {
		ledgerConsistent = false
	}
	// Old aggregate fields cannot hide incomplete entries in the newer ledger.
	partial, uninspected := 0, 0
	if value := source.Completeness.PartiallyInspected; value != nil && *value > 0 {
		partial = *value
	}
	if value := source.Completeness.EntirelyUninspected; value != nil && *value > 0 {
		uninspected = *value
	}
	if partial+uninspected > 0 {
		ledgerConsistent = false
		if report.Coverage.FilesSkipped < partial+uninspected {
			report.Coverage.FilesSkipped = partial + uninspected
		}
	}
	if source.Completeness.FilesSeen != nil && source.Completeness.FullyInspected != nil && source.Completeness.PartiallyInspected != nil && source.Completeness.EntirelyUninspected != nil && *source.Completeness.FilesSeen != *source.Completeness.FullyInspected+*source.Completeness.PartiallyInspected+*source.Completeness.EntirelyUninspected {
		ledgerConsistent = false
	}
	gapFiles := map[string]bool{}
	for _, group := range [][]spectorScopeGap{source.Completeness.LedgerExceptions, source.Completeness.ScopeExclusions} {
		for _, gap := range group {
			ledgerConsistent = false
			file, reason := gap.Path, gap.ReasonCode
			if file == "" {
				file = "."
			}
			if gap.Message != "" {
				if reason != "" {
					reason += ": "
				}
				reason += gap.Message
			}
			if reason == "" {
				reason = "upstream_scope_or_ledger_exception"
			}
			gapFiles[file] = true
			if len(report.Coverage.Skipped) < MaxFiles {
				report.Coverage.Skipped = append(report.Coverage.Skipped, Skipped{File: Redact(file), Reason: Redact(reason)})
			}
		}
	}
	if report.Coverage.FilesSkipped < len(gapFiles) {
		report.Coverage.FilesSkipped = len(gapFiles)
	}
	if report.Coverage.FilesScanned+report.Coverage.FilesSkipped > report.Coverage.FilesSeen {
		report.Coverage.FilesSeen = report.Coverage.FilesScanned + report.Coverage.FilesSkipped
	}
	for index, issue := range source.Issues {
		if len(report.Findings) >= 1000 {
			report.Warnings = append(report.Warnings, "SkillSpector 告警超过 1000 条，报告展示已截断")
			report.Coverage.Skipped = append(report.Coverage.Skipped, Skipped{File: ".", Reason: "finding_count_limit"})
			break
		}
		line := issue.Location.StartLine
		if line <= 0 {
			line = issue.Location.Line
		}
		if line <= 0 {
			line = issue.LineNumber
		}
		if line < 0 {
			line = 0
		}
		severity := strings.ToLower(issue.Severity)
		switch severity {
		case "critical", "high", "medium", "low", "info":
		default:
			severity = "medium"
		}
		id := Redact(issue.FindingID)
		if id == "" {
			id = Redact(issue.ID)
		}
		if id == "" {
			id = fmt.Sprintf("skillspector-%d", index+1)
		}
		reason := issue.Message
		if reason == "" {
			reason = issue.Explanation
		}
		if reason == "" {
			reason = issue.Description
		}
		if reason == "" {
			reason = issue.Title
		}
		evidence := issue.CodeSnippet
		if evidence == "" {
			evidence = issue.Finding
		}
		if len(issue.Evidence) > 0 && string(issue.Evidence) != "null" {
			if evidence == "" && string(issue.Evidence) != "{}" {
				if err := json.Unmarshal(issue.Evidence, &evidence); err != nil {
					evidence = string(issue.Evidence)
				}
			}
		}
		if evidence == "" {
			evidence = issue.CodeSnippet
		}
		file := issue.Location.File
		if file == "" {
			file = issue.FilePath
		}
		recommendation := issue.Recommendation
		if recommendation == "" {
			recommendation = issue.Remediation
		}
		ruleID := issue.RuleID
		if ruleID == "" {
			ruleID = issue.ID
		}
		report.Findings = append(report.Findings, Finding{
			ID: id, RuleID: Redact(ruleID), Severity: severity,
			Category: Redact(issue.Category), File: Redact(file), Line: line,
			Evidence: Redact(evidence), Reason: Redact(reason), Recommendation: Redact(recommendation),
		})
	}
	if exitCode >= 2 || exitCode < 0 {
		return spectorReportError(report, "execution_failed", "SkillSpector 执行失败，保留的告警不代表完整检查结果")
	}
	if source.ExecutionSuccessful != nil && !*source.ExecutionSuccessful {
		return spectorReportError(report, "execution_failed", "SkillSpector 报告检查执行失败")
	}
	recommendation := strings.ToUpper(source.RiskAssessment.Recommendation)
	switch recommendation {
	case "SAFE", "CAUTION", "DO_NOT_INSTALL":
	default:
		return spectorReportError(report, "invalid_report", "SkillSpector 报告缺少可识别的风险建议")
	}
	if recommendation != "SAFE" {
		report.Warnings = append(report.Warnings, "SkillSpector 原始风险建议："+recommendation)
	}
	complete := false
	if source.Completeness.Complete != nil {
		complete = *source.Completeness.Complete
	} else {
		complete = strings.EqualFold(source.Completeness.Status, "complete") || strings.EqualFold(source.Completeness.Status, "completed")
	}
	if status := strings.ToLower(source.Completeness.Status); status != "" && status != "complete" && status != "completed" {
		complete = false
	}
	if source.ExecutionSuccessful == nil {
		complete = false
		report.Warnings = append(report.Warnings, "报告未提供明确的执行完成状态")
	}
	if source.Completeness.Coverage != nil && *source.Completeness.Coverage != 100 {
		complete = false
	}
	if !ledgerConsistent || report.Coverage.FilesSeen <= 0 || report.Coverage.FilesScanned <= 0 {
		complete = false
		report.Warnings = append(report.Warnings, "报告没有有效的正数文件覆盖，或文件账本字段互相矛盾")
	}
	if allowLLM && (source.Metadata.LLMRequested == nil || !*source.Metadata.LLMRequested || source.Metadata.LLMAvailable == nil || !*source.Metadata.LLMAvailable || source.Metadata.LLMError != "") {
		complete = false
		report.Warnings = append(report.Warnings, "请求的模型辅助分析未完成")
	}
	if len(report.Coverage.Skipped) > 0 || report.Coverage.FilesSkipped > 0 {
		complete = false
	}
	if !complete {
		report.Status = "incomplete"
		report.Recommendation = "incomplete"
		report.Warnings = append(report.Warnings, "SkillSpector 检查范围不完整，不能视为通过")
		return report, nil
	}
	report.Status = "completed"
	report.Recommendation = "review"
	if recommendation == "SAFE" && len(report.Findings) == 0 && exitCode == 0 {
		report.Recommendation = "no_findings"
	}
	// Raw intentionally remains nil: upstream reports may contain source text or
	// opaque credential-bearing fields not covered by our evidence redaction.
	return report, nil
}

type spectorSourceFile struct {
	Path       string
	Size       int64
	Digest     [sha256.Size]byte
	Executable bool
}

// Inventory is read without executing source or following links. The caller
// supplies a privately prepared snapshot; unbounded directory trees are refused.
func spectorSourceInventory(dir string) ([]spectorSourceFile, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := []spectorSourceFile{}
	seen := map[string]bool{}
	var bytes int64
	entries := 0
	err = filepath.WalkDir(dir, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > MaxFiles*32 {
			return errors.New("scanner source contains too many directory entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("scanner source contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("scanner source contains an unreadable or special file")
		}
		relative, err := filepath.Rel(dir, name)
		relative = filepath.ToSlash(relative)
		key := strings.ToLower(relative)
		if err != nil || !safePath(relative) || seen[key] {
			return errors.New("scanner source contains an invalid or conflicting path")
		}
		seen[key] = true
		if len(files) >= MaxFiles || info.Size() < 0 || info.Size() > MaxTotalBytes-bytes {
			return errors.New("scanner source exceeds its file or byte limit")
		}
		bytes += info.Size()
		file, err := root.Open(relative)
		if err != nil {
			return err
		}
		hash := sha256.New()
		count, readErr := io.Copy(hash, io.LimitReader(file, MaxTotalBytes-(bytes-info.Size())+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || count != info.Size() {
			return errors.New("scanner source changed while inventorying its bytes")
		}
		identity := spectorSourceFile{Path: relative, Size: count, Executable: info.Mode()&0111 != 0}
		copy(identity.Digest[:], hash.Sum(nil))
		files = append(files, identity)
		return nil
	})
	if err == nil && len(files) == 0 {
		err = errors.New("scanner source has no regular files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func verifySpectorCoverage(report Report, data []byte, dir string, expected []spectorSourceFile) Report {
	incomplete := func(message string) {
		report.Status, report.Recommendation = "incomplete", "incomplete"
		report.Warnings = append(report.Warnings, message)
	}
	actual, err := spectorSourceInventory(dir)
	if err != nil || len(actual) != len(expected) {
		incomplete("扫描期间来源目录已变化，报告不能用于安装")
	} else {
		for index := range expected {
			if actual[index] != expected[index] {
				incomplete("扫描期间来源目录已变化，报告不能用于安装")
				break
			}
		}
	}
	if report.Coverage.FilesSeen != len(expected) || report.Coverage.FilesScanned != len(expected) {
		incomplete("扫描器的文件覆盖数量与实际来源目录不一致")
	}
	var source spectorReport
	if json.Unmarshal(data, &source) != nil {
		incomplete("无法核对扫描器的实际文件清单")
		return report
	}
	paths := map[string]bool{}
	invalid := false
	for _, component := range source.Components {
		name := component.Path
		if filepath.IsAbs(name) {
			name, err = filepath.Rel(dir, name)
			if err != nil {
				invalid = true
				continue
			}
		}
		name = strings.TrimPrefix(filepath.ToSlash(name), "./")
		if !safePath(name) || paths[name] {
			invalid = true
			continue
		}
		paths[name] = true
	}
	for _, file := range expected {
		if !paths[file.Path] {
			invalid = true
			report.Coverage.Skipped = append(report.Coverage.Skipped, Skipped{File: Redact(file.Path), Reason: "missing_from_scanner_file_inventory"})
		}
	}
	if invalid || len(paths) != len(expected) {
		incomplete("扫描器未返回与实际来源一致的完整文件清单")
	}
	return report
}

// RunSkillSpector invokes a configured local scanner using an argument vector.
// It never invokes a shell or executes any source file from the scanned skill.
func RunSkillSpector(ctx context.Context, dir string, executable string, allowLLM bool) (Report, error) {
	report := emptySpectorReport()
	if strings.TrimSpace(executable) == "" {
		executable = "skillspector"
	}
	command, err := exec.LookPath(executable)
	if err != nil {
		report.Status = "unavailable"
		report.Warnings = append(report.Warnings, "SkillSpector 未安装或配置的程序不可用")
		return report, &spectorError{"unavailable", "SkillSpector 未安装或配置的程序不可用"}
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return spectorReportError(report, "invalid_directory", "扫描来源必须是已准备的普通目录")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return spectorReportError(report, "invalid_directory", "无法确认扫描来源目录")
	}
	expected, err := spectorSourceInventory(dir)
	if err != nil {
		return spectorReportError(report, "invalid_directory", "扫描来源包含链接、特殊文件、无效路径、空目录或超出范围")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"scan", dir, "--format", "json", "--fail-on-incomplete"}
	if !allowLLM {
		args = append(args, "--no-llm")
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.WaitDelay = 2 * time.Second
	stdout := &cappedOutput{limit: spectorStdoutLimit}
	stderr := &cappedOutput{limit: spectorStderrLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := runSpectorCommand(cmd)
	if ctx.Err() != nil {
		report.Status = "incomplete"
		report.Warnings = append(report.Warnings, "SkillSpector 已取消或超时，检查未完成")
		return report, ctx.Err()
	}
	output, outputExceeded := stdout.snapshot()
	logs, logsExceeded := stderr.snapshot()
	if outputExceeded {
		return spectorReportError(report, "output_limit", "SkillSpector JSON 报告超过 8 MiB 限制，检查未完成")
	}
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return spectorReportError(report, "execution_failed", "无法完成 SkillSpector 进程执行")
		}
		exitCode = exitErr.ExitCode()
	}
	report, err = parseSpectorReport(output, exitCode, allowLLM)
	if err == nil {
		report = verifySpectorCoverage(report, output, dir, expected)
	}
	if logsExceeded {
		report.Warnings = append(report.Warnings, "SkillSpector 日志超过 64 KiB，已截断保存范围")
	}
	if len(logs) > 0 {
		report.Warnings = append(report.Warnings, "扫描器日志："+Redact(string(logs)))
	}
	return report, err
}

var _ io.Writer = (*cappedOutput)(nil)
