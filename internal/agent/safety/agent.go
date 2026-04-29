package safety

import (
	"context"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shellmcp"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// CommandRequest represents a command execution request
type CommandRequest struct {
	Command     string
	Reason      string
	Source      string
	Iteration   int
	ContextInfo map[string]string
}

// CommandResult represents the result of command execution
type CommandResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	AuditInfo *AuditInfo
	Output    string // Shell MCP 执行输出
	IsError   bool   // 是否执行出错
}

// AuditInfo contains safety audit information
type AuditInfo struct {
	Allowed     bool
	SafetyLevel string // safe / warning / dangerous
	Reason      string
	Advice      string
	Method      string // rule / llm
}

// Auditor is the interface for safety auditors
type Auditor interface {
	Audit(ctx context.Context, command, reason string) (*AuditResult, error)
}

// CommandExecutor is the interface for command execution
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, command string) (*shellmcp.ExecuteResult, error)
}

// SafetyAgent is the safety command execution agent
type SafetyAgent struct {
	rules     *RuleEngine
	auditor   Auditor
	mcpClient CommandExecutor
}

// NewSafetyAgent creates a new SafetyAgent
func NewSafetyAgent(rules *RuleEngine, auditor Auditor, mcpClient CommandExecutor) *SafetyAgent {
	return &SafetyAgent{
		rules:     rules,
		auditor:   auditor,
		mcpClient: mcpClient,
	}
}

// ExecuteSafeCommand audits and executes a command safely
func (a *SafetyAgent) ExecuteSafeCommand(ctx context.Context, req *CommandRequest) (*CommandResult, error) {
	logger.Info("Starting safety command audit",
		logger.String("command", req.Command),
		logger.String("reason", req.Reason),
	)

	// 1. Rule engine evaluation
	ruleResult := a.rules.Evaluate(req.Command)

	logger.Info("Rule engine evaluation completed",
		logger.String("command", req.Command),
		logger.String("action", ruleResult.Action),
		logger.String("reason", ruleResult.Reason),
	)

	// 2. Branch based on rule result
	switch ruleResult.Action {
	case "allow":
		// Whitelist command, execute directly (skip LLM)
		logger.Info("Whitelist command, skipping LLM audit",
			logger.String("command", req.Command),
		)
		return a.executeCommand(ctx, req, &AuditInfo{
			Allowed:     true,
			SafetyLevel: "safe",
			Reason:      ruleResult.Reason,
			Advice:      "",
			Method:      "rule",
		})

	case "deny":
		// Blacklist command, reject immediately (no LLM)
		advice := generateDenyAdvice(req.Command)
		logger.Warn("Blacklist command rejected",
			logger.String("command", req.Command),
			logger.String("reason", ruleResult.Reason),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      ruleResult.Reason,
				Advice:      advice,
				Method:      "rule",
			},
		}, nil

	case "unknown":
		// Unknown command, need LLM audit
		return a.auditAndExecute(ctx, req)

	default:
		// Unknown action, fallback to deny
		logger.Error("Unknown rule evaluation result",
			logger.String("command", req.Command),
			logger.String("action", ruleResult.Action),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      "Rule engine returned unknown action: " + ruleResult.Action,
				Advice:      "Please contact administrator to check rule configuration",
				Method:      "rule",
			},
		}, nil
	}
}

// auditAndExecute performs LLM audit and executes if safe
func (a *SafetyAgent) auditAndExecute(ctx context.Context, req *CommandRequest) (*CommandResult, error) {
	// LLM unavailable, fallback to deny
	if a.auditor == nil {
		logger.Warn("LLM auditor unavailable, defaulting to deny for unknown command",
			logger.String("command", req.Command),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      "Unknown command and LLM auditor unavailable",
				Advice:      "Please configure LLM auditor or use whitelist commands",
				Method:      "rule",
			},
		}, nil
	}

	// Call LLM audit
	auditResult, err := a.auditor.Audit(ctx, req.Command, req.Reason)
	if err != nil {
		logger.Error("LLM audit failed",
			logger.String("command", req.Command),
			logger.Err(err),
		)
		return nil, fmt.Errorf("llm audit failed: %w", err)
	}

	// LLM returned nil (timeout/failure), fallback to deny
	if auditResult == nil {
		logger.Warn("LLM audit returned nil, fallback to deny",
			logger.String("command", req.Command),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      "LLM audit timeout or failure",
				Advice:      "Please retry later or use whitelist commands",
				Method:      "llm",
			},
		}, nil
	}

	logger.Info("LLM audit completed",
		logger.String("command", req.Command),
		logger.String("safety_level", auditResult.SafetyLevel),
		logger.String("reason", auditResult.Reason),
	)

	// Decide based on LLM audit result
	switch auditResult.SafetyLevel {
	case "safe", "warning":
		// Allow execution
		return a.executeCommand(ctx, req, &AuditInfo{
			Allowed:     true,
			SafetyLevel: auditResult.SafetyLevel,
			Reason:      auditResult.Reason,
			Advice:      auditResult.Advice,
			Method:      "llm",
		})

	case "dangerous":
		// Reject execution
		logger.Warn("LLM determined command is dangerous, rejecting",
			logger.String("command", req.Command),
			logger.String("reason", auditResult.Reason),
			logger.String("advice", auditResult.Advice),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      auditResult.Reason,
				Advice:      auditResult.Advice,
				Method:      "llm",
			},
		}, nil

	default:
		// Unknown safety level, fallback to deny
		logger.Error("LLM returned unknown safety level",
			logger.String("command", req.Command),
			logger.String("safety_level", auditResult.SafetyLevel),
		)
		return &CommandResult{
			AuditInfo: &AuditInfo{
				Allowed:     false,
				SafetyLevel: "dangerous",
				Reason:      "LLM returned unknown safety level: " + auditResult.SafetyLevel,
				Advice:      "Please contact administrator to check LLM configuration",
				Method:      "llm",
			},
		}, nil
	}
}

// executeCommand executes the command
func (a *SafetyAgent) executeCommand(ctx context.Context, req *CommandRequest, auditInfo *AuditInfo) (*CommandResult, error) {
	logger.Info("Executing command",
		logger.String("command", req.Command),
		logger.String("safety_level", auditInfo.SafetyLevel),
	)

	execResult, err := a.mcpClient.ExecuteCommand(ctx, req.Command)
	if err != nil {
		logger.Error("Command execution failed",
			logger.String("command", req.Command),
			logger.Err(err),
		)
		return nil, fmt.Errorf("execute command: %w", err)
	}

	logger.Info("Command execution completed",
		logger.String("command", req.Command),
		logger.String("summary", execResult.Summary),
		logger.Any("is_error", execResult.IsError),
	)

	return &CommandResult{
		Stdout:    execResult.Output,
		Stderr:    "",
		ExitCode:  exitCodeFromExecuteResult(execResult),
		AuditInfo: auditInfo,
		Output:    execResult.Output,
		IsError:   execResult.IsError,
	}, nil
}

func exitCodeFromExecuteResult(result *shellmcp.ExecuteResult) int {
	if result == nil {
		return 1
	}
	if result.IsError {
		return 1
	}
	return 0
}

// ExecuteSimple is a simplified command execution (returns output string or error description)
func (a *SafetyAgent) ExecuteSimple(ctx context.Context, command, reason string) (string, error) {
	req := &CommandRequest{
		Command: command,
		Reason:  reason,
		Source:  "simple",
	}

	result, err := a.ExecuteSafeCommand(ctx, req)
	if err != nil {
		return "", err
	}

	// If audit failed, return formatted rejection message
	if !result.AuditInfo.Allowed {
		msg := fmt.Sprintf("Command rejected: %s\n", command)
		msg += fmt.Sprintf("Reason: %s\n", result.AuditInfo.Reason)
		if result.AuditInfo.Advice != "" {
			msg += fmt.Sprintf("Advice: %s\n", result.AuditInfo.Advice)
		}
		return msg, nil
	}

	// Return aggregated stdout
	return result.Stdout, nil
}

// generateDenyAdvice generates advice for denied commands
func generateDenyAdvice(command string) string {
	lowerCmd := strings.ToLower(command)

	// rm related commands
	if strings.Contains(lowerCmd, "rm ") || strings.HasPrefix(lowerCmd, "rm") {
		return "Use 'du -sh <path>' to check directory size, or 'ls -la <path>' to verify contents before operation"
	}

	// mkfs related commands
	if strings.Contains(lowerCmd, "mkfs") {
		return "Use 'lsblk' or 'fdisk -l' to check disk partition information and confirm target device"
	}

	// dd related commands
	if strings.Contains(lowerCmd, "dd ") || strings.HasPrefix(lowerCmd, "dd") {
		return "Use 'lsblk' to check block device information and confirm input/output file paths"
	}

	// shutdown/reboot related commands
	if strings.Contains(lowerCmd, "shutdown") || strings.Contains(lowerCmd, "reboot") || strings.Contains(lowerCmd, "poweroff") {
		return "Use 'uptime' to check system uptime, or 'systemctl status' to check service status"
	}

	// kill related commands
	if strings.Contains(lowerCmd, "kill") || strings.Contains(lowerCmd, "pkill") {
		return "Use 'ps aux | grep <pattern>' to check process information, or 'top' to check system resource usage"
	}

	// chmod 777 related commands
	if strings.Contains(lowerCmd, "chmod 777") || strings.Contains(lowerCmd, "chmod -R 777") {
		return "Use 'ls -la <path>' to check current permissions, consider using stricter permissions like 755 or 644"
	}

	// iptables related commands
	if strings.Contains(lowerCmd, "iptables -f") || strings.Contains(lowerCmd, "iptables --flush") {
		return "Use 'iptables -L -v -n' to view current rules, or 'iptables -L --line-numbers' to view rule numbers"
	}

	// curl | sh related commands
	if strings.Contains(lowerCmd, "curl") && strings.Contains(lowerCmd, "|") && (strings.Contains(lowerCmd, "sh") || strings.Contains(lowerCmd, "bash")) {
		return "Download script locally first using 'curl -o script.sh <url>', then review manually before execution"
	}

	// eval/exec related commands
	if strings.Contains(lowerCmd, "eval ") || strings.Contains(lowerCmd, "exec ") {
		return "Check command source to avoid executing untrusted dynamic code"
	}

	// Default advice
	return "Use read-only commands from the whitelist, or contact administrator to add necessary safety rules"
}
