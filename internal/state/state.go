package state

import (
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// State Graph 流转状态
type State struct {
	mu sync.Mutex

	UserInput         string
	K8sInfo           *K8sInfo
	ReasoningHistory  []ReasoningStep
	CompressedSummary string
	CompressThreshold int
	IterationCount    int
	MaxIterations     int
	AnalysisResult    *AnalysisResult
	LastError         error
	LastAction        string
	// 内部追踪
	CommandExecutions []CommandExecution
	BlockedCommands   []BlockedCommand
	// 验证阶段相关字段
	VerifyPhase          bool // 是否处于验证迭代阶段
	VerifyIterationCount int  // 验证阶段已用迭代次数
	MaxVerifyIterations  int  // 验证阶段最大迭代数（来自 config）
	// 缓存命中统计（按轮次）
	CacheStats map[int]*RoundCacheStats
	// Skill support
	ActiveSkillName       string
	ActiveSkillContent    string
	TotalPromptTokens     int
	TotalCompletionTokens int
	TotalTokens           int
}

// NewState 创建新的 State
// 参数 maxIterations 默认 10，compressThreshold 默认 4
func NewState(userInput string, maxIterations, compressThreshold int) *State {
	if maxIterations <= 0 {
		maxIterations = 10
	}
	if compressThreshold <= 0 {
		compressThreshold = 4
	}

	return &State{
		UserInput:         userInput,
		MaxIterations:     maxIterations,
		CompressThreshold: compressThreshold,
		IterationCount:    0,
		ReasoningHistory:  make([]ReasoningStep, 0),
		CommandExecutions: make([]CommandExecution, 0),
		BlockedCommands:   make([]BlockedCommand, 0),
		CacheStats:        make(map[int]*RoundCacheStats),
	}
}

// ActivateSkill 为当前会话标记并载入技能内容。仅允许首次激活，后续调用不改变已激活的技能。
func (s *State) ActivateSkill(name string, content string) {
	if s == nil {
		return
	}
	// 仅在尚未激活技能时生效
	if s.HasActiveSkill() {
		return
	}
	s.ActiveSkillName = name
	s.ActiveSkillContent = content
}

// HasActiveSkill 判断当前会话是否已经激活了技能
func (s *State) HasActiveSkill() bool {
	if s == nil {
		return false
	}
	return s.ActiveSkillName != ""
}

// AddReasoningStep 添加推理步骤
func (s *State) AddReasoningStep(step ReasoningStep) {
	if s == nil {
		return
	}
	s.ReasoningHistory = append(s.ReasoningHistory, step)
}

// AddFinding 添加诊断发现
func (s *State) AddFinding(f Finding) {
	if s == nil || s.AnalysisResult == nil {
		return
	}
	s.AnalysisResult.Findings = append(s.AnalysisResult.Findings, f)
}

// AddCommandExecution 记录命令执行
func (s *State) AddCommandExecution(command string, success bool, output string, isVerifyPhase bool) {
	if s == nil {
		return
	}
	s.AddCommandExecutionRecord(CommandExecution{
		Command:       command,
		Success:       success,
		Output:        output,
		Timestamp:     time.Now(),
		IsVerifyPhase: isVerifyPhase,
	})
}

// AddCommandExecutionRecord 记录完整命令执行信息
func (s *State) AddCommandExecutionRecord(exec CommandExecution) {
	if s == nil {
		return
	}
	if exec.Timestamp.IsZero() {
		exec.Timestamp = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CommandExecutions = append(s.CommandExecutions, exec)
}

// AddBlockedCommand 记录被拒绝的命令
func (s *State) AddBlockedCommand(cmd BlockedCommand) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BlockedCommands = append(s.BlockedCommands, cmd)
}

// ShouldContinue 判断是否应继续迭代
// 返回 s.IterationCount < s.MaxIterations && s.AnalysisResult == nil
func (s *State) ShouldContinue() bool {
	if s == nil {
		return false
	}
	return s.IterationCount < s.MaxIterations && s.AnalysisResult == nil
}

// IncrementIteration 增加迭代计数
func (s *State) IncrementIteration() {
	if s == nil {
		return
	}
	s.IterationCount++
}

// GetRecentSteps 获取最近 N 步推理历史
func (s *State) GetRecentSteps(n int) []ReasoningStep {
	if s == nil || n <= 0 {
		return nil
	}

	historyLen := len(s.ReasoningHistory)
	if historyLen == 0 {
		return nil
	}

	if n >= historyLen {
		result := make([]ReasoningStep, historyLen)
		copy(result, s.ReasoningHistory)
		return result
	}

	result := make([]ReasoningStep, n)
	copy(result, s.ReasoningHistory[historyLen-n:])
	return result
}

// SetAnalysisResult 设置分析结果
func (s *State) SetAnalysisResult(result *AnalysisResult) {
	if s == nil {
		return
	}
	s.AnalysisResult = result
}

// GetIterationCount 获取当前迭代计数
func (s *State) GetIterationCount() int {
	if s == nil {
		return 0
	}
	return s.IterationCount
}

// GetMaxIterations 获取最大迭代次数
func (s *State) GetMaxIterations() int {
	if s == nil {
		return 0
	}
	return s.MaxIterations
}

// GetReasoningHistory 获取完整的推理历史
func (s *State) GetReasoningHistory() []ReasoningStep {
	if s == nil {
		return nil
	}
	result := make([]ReasoningStep, len(s.ReasoningHistory))
	copy(result, s.ReasoningHistory)
	return result
}

// GetCommandExecutions 获取命令执行记录
func (s *State) GetCommandExecutions() []CommandExecution {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]CommandExecution, len(s.CommandExecutions))
	copy(result, s.CommandExecutions)
	return result
}

// GetBlockedCommands 获取被拒绝的命令列表
func (s *State) GetBlockedCommands() []BlockedCommand {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]BlockedCommand, len(s.BlockedCommands))
	copy(result, s.BlockedCommands)
	return result
}

// SetK8sInfo 设置 K8s 集群信息
func (s *State) SetK8sInfo(info *K8sInfo) {
	if s == nil {
		return
	}
	s.K8sInfo = info
}

// GetK8sInfo 获取 K8s 集群信息
func (s *State) GetK8sInfo() *K8sInfo {
	if s == nil {
		return nil
	}
	return s.K8sInfo
}

// SetLastError 设置最后错误
func (s *State) SetLastError(err error) {
	if s == nil {
		return
	}
	s.LastError = err
}

// GetLastError 获取最后错误
func (s *State) GetLastError() error {
	if s == nil {
		return nil
	}
	return s.LastError
}

// SetLastAction 设置最后操作
func (s *State) SetLastAction(action string) {
	if s == nil {
		return
	}
	s.LastAction = action
}

// GetLastAction 获取最后操作
func (s *State) GetLastAction() string {
	if s == nil {
		return ""
	}
	return s.LastAction
}

// SetCompressedSummary 设置压缩摘要
func (s *State) SetCompressedSummary(summary string) {
	if s == nil {
		return
	}
	s.CompressedSummary = summary
}

// GetCompressedSummary 获取压缩摘要
func (s *State) GetCompressedSummary() string {
	if s == nil {
		return ""
	}
	return s.CompressedSummary
}

// ShouldCompress 判断是否需要压缩历史
func (s *State) ShouldCompress() bool {
	if s == nil {
		return false
	}
	return len(s.ReasoningHistory) >= s.CompressThreshold
}

// CreateNewReasoningStep 创建一个新的推理步骤
func (s *State) CreateNewReasoningStep(thought, decision string) ReasoningStep {
	return ReasoningStep{
		Iteration: s.IterationCount,
		Timestamp: time.Now(),
		Thought:   thought,
		Decision:  decision,
		ToolCalls: make([]ToolCall, 0),
	}
}

// EnterVerifyPhase 进入验证阶段（由 graph 路由调用）
func (s *State) EnterVerifyPhase(maxVerifyIter int) {
	if s == nil {
		return
	}
	s.VerifyPhase = true
	s.VerifyIterationCount = 0
	s.MaxVerifyIterations = maxVerifyIter
}

// IncrementVerifyIteration 自增验证迭代计数，返回是否已超限
func (s *State) IncrementVerifyIteration() bool {
	if s == nil {
		return true
	}
	s.VerifyIterationCount++
	return s.VerifyIterationCount > s.MaxVerifyIterations
}

// GetVerifyPhaseExecutions 获取验证阶段的命令执行记录
func (s *State) GetVerifyPhaseExecutions() []CommandExecution {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []CommandExecution
	for _, e := range s.CommandExecutions {
		if e.IsVerifyPhase {
			result = append(result, e)
		}
	}
	return result
}

// GetLastReasoningStep 获取最后一个推理步骤
func (s *State) GetLastReasoningStep() *ReasoningStep {
	if s == nil || len(s.ReasoningHistory) == 0 {
		return nil
	}
	return &s.ReasoningHistory[len(s.ReasoningHistory)-1]
}

// HasUnverifiedRecommendations 判断是否有未验证的建议
func (s *State) HasUnverifiedRecommendations() bool {
	if s.AnalysisResult == nil {
		return false
	}
	for _, r := range s.AnalysisResult.Recommendations {
		if !r.Verified {
			return true
		}
	}
	return false
}

// RecordCacheHit 记录当前轮次的缓存命中
func (s *State) RecordCacheHit(iteration int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.getOrCreateCacheStats(iteration)
	stats.TotalCalls++
	stats.CacheHits++
}

// RecordCacheMiss 记录当前轮次的缓存未命中
func (s *State) RecordCacheMiss(iteration int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.getOrCreateCacheStats(iteration)
	stats.TotalCalls++
}

// AccumulateTokenUsage 累加 LLM token 使用量
func (s *State) AccumulateTokenUsage(usage *schema.TokenUsage) {
	if s == nil || usage == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalPromptTokens += usage.PromptTokens
	s.TotalCompletionTokens += usage.CompletionTokens
	s.TotalTokens += usage.TotalTokens
}

// GetRoundCacheStats 获取指定轮次的缓存统计
func (s *State) GetRoundCacheStats(iteration int) *RoundCacheStats {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CacheStats[iteration]
}

// getOrCreateCacheStats 获取或创建指定轮次的缓存统计（需持有锁）
func (s *State) getOrCreateCacheStats(iteration int) *RoundCacheStats {
	if s.CacheStats[iteration] == nil {
		s.CacheStats[iteration] = &RoundCacheStats{}
	}
	return s.CacheStats[iteration]
}
