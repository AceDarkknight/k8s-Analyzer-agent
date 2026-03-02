// Package analysis 提供主分析 Agent，基于 Eino 框架实现 Graph 编排
package analysis

import (
	"fmt"
	"sort"
	"time"
)

// State 定义 Graph 的状态结构体
// 包含用户输入、K8s 信息、分析结果和迭代计数
type State struct {
	// UserInput 用户输入的查询或指令
	UserInput string

	// K8sInfo 从 K8s 集群收集的信息
	K8sInfo *K8sInfo

	// AnalysisResult 分析结果
	AnalysisResult *AnalysisResult

	// IterationCount 当前迭代次数（防止死循环）
	IterationCount int

	// MaxIterations 最大迭代次数限制
	MaxIterations int

	// LastAction 上一次执行的动作
	LastAction string

	// LastError 上一次的错误信息
	LastError error

	// ReasoningHistory 推理历史记录
	// 用于在迭代中保持上下文
	ReasoningHistory []ReasoningStep
}

// ReasoningStep 表示推理过程中的单个步骤
// 包含 LLM 的思考、决策、工具调用和执行结果
type ReasoningStep struct {
	Iteration   int        `json:"iteration"`             // 迭代次数
	Timestamp   time.Time  `json:"timestamp"`             // 时间戳
	Thought     string     `json:"thought"`               // LLM 对此步骤的推理
	Decision    string     `json:"decision"`              // 决策结果 (continue, report)
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`  // 如果决策涉及工具调用
	Observation string     `json:"observation,omitempty"` // 执行结果/输出的摘要
}

// K8sInfo 存储 K8s 集群信息
type K8sInfo struct {
	// Namespace 当前操作的命名空间
	Namespace string

	// Resources 统一存储为切片。Key 是资源类型（如 "Pods", "Deployments"）。
	// 即使是单个对象也会被封装为长度为 1 的切片。
	Resources map[string][]any

	// NetworkInfo 网络信息
	NetworkInfo *NetworkInfo
}

// SetResources 直接将资源类型设置为提供的 items 切片
// items 可以是多个对象，或者使用 slice... 语法传入一个切片
func (k *K8sInfo) SetResources(resourceType string, items ...any) {
	if k.Resources == nil {
		k.Resources = make(map[string][]any)
	}
	k.Resources[resourceType] = items
}

// AppendResource 向 Resources[resourceType] 切片中追加一个或多个 items
// 负责切片的初始化
func (k *K8sInfo) AppendResource(resourceType string, items ...any) {
	if k.Resources == nil {
		k.Resources = make(map[string][]any)
	}
	k.Resources[resourceType] = append(k.Resources[resourceType], items...)
}

// GetSummary 获取资源汇总信息
// 获取 Resources 的所有 Key 并排序以保证输出顺序
// 遍历每个资源，直接使用 len(slice) 获取计数
// 拼接成最终摘要，例如："命名空间: default, Pods: 12, Deployments: 2, Logs: 100"
func (k *K8sInfo) GetSummary() string {
	if len(k.Resources) == 0 {
		if k.Namespace != "" {
			return "命名空间: " + k.Namespace
		}
		return ""
	}

	// 获取所有 Key 并排序
	keys := make([]string, 0, len(k.Resources))
	for key := range k.Resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// 构建摘要字符串
	parts := make([]string, 0, len(k.Resources)+1)

	// 先添加命名空间
	if k.Namespace != "" {
		parts = append(parts, "命名空间: "+k.Namespace)
	}

	// 添加每个资源类型的计数
	for _, key := range keys {
		count := len(k.Resources[key])
		parts = append(parts, fmt.Sprintf("%s: %d", key, count))
	}

	// 用 ", " 连接所有部分
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}

	return result
}

// PodInfo Pod 信息
type PodInfo struct {
	Name       string
	Namespace  string
	Status     string
	Phase      string
	Restarts   int32
	NodeName   string
	CreateTime time.Time
	Labels     map[string]string
}

// DeploymentInfo Deployment 信息
type DeploymentInfo struct {
	Name              string
	Namespace         string
	Replicas          int32
	AvailableReplicas int32
	ReadyReplicas     int32
	UpdatedReplicas   int32
}

// PortInfo 端口信息
type PortInfo struct {
	Name     string
	Port     int32
	Protocol string
}

// EventInfo 事件信息
type EventInfo struct {
	Type      string
	Reason    string
	Message   string
	Component string
	Timestamp time.Time
}

// LogInfo 日志信息
type LogInfo struct {
	PodName   string
	Container string
	Timestamp time.Time
	Message   string
}

// NetworkInfo 网络信息
type NetworkInfo struct {
	PodIPs       []string
	ServiceIPs   []string
	Connectivity []ConnectivityInfo
}

// ConnectivityInfo 连通性信息
type ConnectivityInfo struct {
	Source  string
	Target  string
	Success bool
	Latency time.Duration
	Error   string
	Output  string // 命令输出
}

// AnalysisResult 分析结果
type AnalysisResult struct {
	// Summary 分析摘要
	Summary string `json:"summary"`

	// Findings 发现的问题列表
	Findings []Finding `json:"findings"`

	// Recommendations 建议列表
	Recommendations []Recommendation `json:"recommendations"`

	// ExecutedCommands 已执行的命令列表
	ExecutedCommands []CommandExecution `json:"executed_commands"`

	// Status 分析状态
	Status AnalysisStatus `json:"status"`
}

// Finding 发现的问题
type Finding struct {
	Severity  string    `json:"severity"` // Critical, High, Medium, Low, Info
	Resource  string    `json:"resource"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// Recommendation 建议
type Recommendation struct {
	Action   string `json:"action"`
	Reason   string `json:"reason"`
	Priority string `json:"priority"`
	Command  string `json:"command"` // 可选的修复命令
}

// CommandExecution 命令执行记录
type CommandExecution struct {
	Command   string
	Output    string
	Success   bool
	Timestamp time.Time
}

// AnalysisStatus 分析状态
type AnalysisStatus string

const (
	StatusInProgress AnalysisStatus = "in_progress"
	StatusCompleted  AnalysisStatus = "completed"
	StatusFailed     AnalysisStatus = "failed"
	StatusPartial    AnalysisStatus = "partial"
)

// Decision 决策类型
type Decision string

const (
	// DecisionContinue 继续执行命令
	DecisionContinue Decision = "continue"

	// DecisionDeepQuery 深入查询更多信息
	DecisionDeepQuery Decision = "deep_query"

	// DecisionReport 生成报告
	DecisionReport Decision = "report"

	// DecisionError 发生错误
	DecisionError Decision = "error"
)

// NewState 创建新的状态
func NewState(userInput string) *State {
	return &State{
		UserInput: userInput,
		K8sInfo:   &K8sInfo{},
		AnalysisResult: &AnalysisResult{
			Status:           StatusInProgress,
			Findings:         make([]Finding, 0),
			Recommendations:  make([]Recommendation, 0),
			ExecutedCommands: make([]CommandExecution, 0),
		},
		IterationCount:   0,
		MaxIterations:    10,                       // 默认最大迭代次数
		ReasoningHistory: make([]ReasoningStep, 0), // 初始化推理历史
	}
}

// IncrementIteration 增加迭代计数
func (s *State) IncrementIteration() error {
	s.IterationCount++
	if s.IterationCount > s.MaxIterations {
		return fmt.Errorf("maximum iterations (%d) exceeded", s.MaxIterations)
	}
	return nil
}

// ShouldContinue 检查是否应该继续执行
func (s *State) ShouldContinue() bool {
	return s.IterationCount < s.MaxIterations && s.AnalysisResult.Status == StatusInProgress
}

// AddFinding 添加发现
func (s *State) AddFinding(severity, resource, message string) {
	finding := Finding{
		Severity:  severity,
		Resource:  resource,
		Message:   message,
		Timestamp: time.Now(),
	}
	s.AnalysisResult.Findings = append(s.AnalysisResult.Findings, finding)
}

// AddRecommendation 添加建议
func (s *State) AddRecommendation(action, reason, priority, command string) {
	rec := Recommendation{
		Action:   action,
		Reason:   reason,
		Priority: priority,
		Command:  command,
	}
	s.AnalysisResult.Recommendations = append(s.AnalysisResult.Recommendations, rec)
}

// AddCommandExecution 添加命令执行记录
func (s *State) AddCommandExecution(command, output string, success bool) {
	exec := CommandExecution{
		Command:   command,
		Output:    output,
		Success:   success,
		Timestamp: time.Now(),
	}
	s.AnalysisResult.ExecutedCommands = append(s.AnalysisResult.ExecutedCommands, exec)
}

// SetStatus 设置分析状态
func (s *State) SetStatus(status AnalysisStatus) {
	s.AnalysisResult.Status = status
}

// GetErrorSummary 获取错误摘要
func (s *State) GetErrorSummary() string {
	if s.LastError == nil {
		return ""
	}
	return s.LastError.Error()
}

// AddReasoningStep 添加一个新的推理步骤到历史记录
// 参数为 LLM 的思考过程、决策结果和工具调用列表
func (s *State) AddReasoningStep(thought, decision string, toolCalls []ToolCall) {
	step := ReasoningStep{
		Iteration: len(s.ReasoningHistory) + 1,
		Timestamp: time.Now(),
		Thought:   thought,
		Decision:  decision,
		ToolCalls: toolCalls,
	}
	s.ReasoningHistory = append(s.ReasoningHistory, step)
}

// UpdateLastStepObservation 更新最后一个推理步骤的观察结果
// 用于在 ActionNode 执行工具调用后，将执行结果写入状态
func (s *State) UpdateLastStepObservation(observation string) {
	if len(s.ReasoningHistory) > 0 {
		lastIndex := len(s.ReasoningHistory) - 1
		s.ReasoningHistory[lastIndex].Observation = observation
	}
}

// GetLastReasoningStep 获取最后一个推理步骤
// 返回 nil 如果历史记录为空
func (s *State) GetLastReasoningStep() *ReasoningStep {
	if len(s.ReasoningHistory) == 0 {
		return nil
	}
	lastIndex := len(s.ReasoningHistory) - 1
	return &s.ReasoningHistory[lastIndex]
}
