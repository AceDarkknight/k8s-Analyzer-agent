package safety

import (
	"os"
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"gopkg.in/yaml.v3"
)

// SafetyRulesConfig 安全规则配置文件结构
type SafetyRulesConfig struct {
	Whitelist struct {
		Commands []string `yaml:"commands"`
	} `yaml:"whitelist"`
	Blacklist struct {
		Patterns []string `yaml:"patterns"`
	} `yaml:"blacklist"`
}

// RuleEngine 规则引擎
type RuleEngine struct {
	whitelist []string         // 白名单命令前缀
	blacklist []*regexp.Regexp // 黑名单正则模式
}

// RuleResult 规则评估结果
type RuleResult struct {
	Action string // "allow" / "deny" / "unknown"
	Reason string
}

// NewRuleEngine 从配置文件加载规则引擎
func NewRuleEngine(configPath string) (*RuleEngine, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config SafetyRulesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return NewRuleEngineFromConfig(config.Whitelist.Commands, config.Blacklist.Patterns)
}

// NewRuleEngineFromConfig 从已解析的配置创建（方便测试）
func NewRuleEngineFromConfig(whitelist []string, blacklistPatterns []string) (*RuleEngine, error) {
	engine := &RuleEngine{
		whitelist: whitelist,
		blacklist: make([]*regexp.Regexp, 0, len(blacklistPatterns)),
	}

	for _, pattern := range blacklistPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			logger.Warn("黑名单正则编译失败，跳过该规则",
				logger.String("pattern", pattern),
				logger.Err(err),
			)
			continue
		}
		engine.blacklist = append(engine.blacklist, re)
	}

	return engine, nil
}

// Evaluate 评估命令安全性
func (r *RuleEngine) Evaluate(command string) RuleResult {
	normalized := normalizeCommand(command)

	// 空命令处理
	if normalized == "" {
		return RuleResult{
			Action: "unknown",
			Reason: "空命令，需要进一步审计",
		}
	}

	// 1. 白名单前缀匹配
	for _, prefix := range r.whitelist {
		if strings.HasPrefix(normalized, prefix) {
			return RuleResult{
				Action: "allow",
				Reason: "命令在白名单中",
			}
		}
	}

	// 2. 黑名单正则匹配
	for _, re := range r.blacklist {
		if re.MatchString(normalized) {
			return RuleResult{
				Action: "deny",
				Reason: "匹配黑名单规则: " + re.String(),
			}
		}
	}

	// 3. 都不匹配
	return RuleResult{
		Action: "unknown",
		Reason: "未知命令，需要进一步审计",
	}
}

// normalizeCommand 命令标准化
func normalizeCommand(cmd string) string {
	// 1. 去除首尾空格
	cmd = strings.TrimSpace(cmd)

	// 2. 合并多个连续空格为单个空格
	// 使用 strings.Fields 分割后再 Join
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}

	// 3. 首个 token 转小写
	fields[0] = strings.ToLower(fields[0])

	return strings.Join(fields, " ")
}
