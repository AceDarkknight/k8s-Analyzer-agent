package gateway

// KubectlRequest Gateway 请求结构
type KubectlRequest struct {
	Verb      string          `json:"verb"`
	Resource  string          `json:"resource"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name,omitempty"`
	Options   *KubectlOptions `json:"options,omitempty"`
	Output    string          `json:"output,omitempty"`
	Mode      string          `json:"mode"` // 固定 "structured"
}

// KubectlOptions kubectl 命令选项
type KubectlOptions struct {
	LabelSelector string `json:"labelSelector,omitempty"`
	FieldSelector string `json:"fieldSelector,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Container     string `json:"container,omitempty"`
	TailLines     int    `json:"tailLines,omitempty"`
	Since         string `json:"since,omitempty"`
	AllNamespaces bool   `json:"allNamespaces,omitempty"`
	Output        string `json:"output,omitempty"`
}

// KubectlResponse Gateway 响应结构
type KubectlResponse struct {
	RequestID         string `json:"request_id"`
	Status            string `json:"status"`     // success / error
	ExitCode          int    `json:"exit_code"`
	Stdout            string `json:"stdout"`
	Stderr            string `json:"stderr"`
	Truncated         bool   `json:"truncated"`
	DurationMs        int    `json:"duration_ms"`
	ResponseSizeBytes int    `json:"response_size_bytes"`
	BlockedReason     string `json:"blocked_reason"`
}
