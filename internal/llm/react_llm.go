package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm/promptregistry"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

// SafeCommandExecutor 安全命令执行接口（避免循环依赖）
type SafeCommandExecutor interface {
	ExecuteSafeCommand(ctx context.Context, command, reason string) (string, error)
}

// SafeCommandExecutorWithResult 扩展接口，返回完整 CommandResult
type SafeCommandExecutorWithResult interface {
	SafeCommandExecutor
	ExecuteSafeCommandWithResult(ctx context.Context, command, reason string) (*safety.CommandResult, error)
}

// ReActLLM ReAct Agent 实现
type ReActLLM struct {
	router                 *LLMRouter
	gateway                *gateway.GatewayClient
	safeExecutor           SafeCommandExecutor
	safeExecutorWithResult SafeCommandExecutorWithResult // 新增
	recorder               *trc.TaskRecorder
	promptReg              *promptregistry.PromptRegistry
}

// NewReActLLM 创建 ReAct LLM
func NewReActLLM(router *LLMRouter, gw *gateway.GatewayClient, safeExecutor SafeCommandExecutor, promptReg *promptregistry.PromptRegistry) *ReActLLM {
	return &ReActLLM{
		router:       router,
		gateway:      gw,
		safeExecutor: safeExecutor,
		promptReg:    promptReg,
	}
}

func (r *ReActLLM) SetRecorder(recorder *trc.TaskRecorder) {
	r.recorder = recorder
	if withResult, ok := r.safeExecutor.(SafeCommandExecutorWithResult); ok {
		r.safeExecutorWithResult = withResult
	}
}

// GetSystemPrompt 优先从 Prompt Registry 构建，失败时回退到硬编码模板。
func (r *ReActLLM) GetSystemPrompt(ctx context.Context) string {
	if r != nil && r.promptReg != nil {
		prompt, err := r.promptReg.BuildSystemPrompt(ctx, "reactSystem", promptregistry.VersionDefault)
		if err == nil && prompt != "" {
			return prompt
		}
		if err != nil {
			logger.Warn("prompt registry build failed for reactSystem, fallback to legacy", logger.Err(err))
		}
	}
	return BuildReActSystemPrompt()
}

// listPodsInput list_pods 工具输入参数
type listPodsInput struct {
	Namespace     string `json:"namespace" jsonschema:"description=命名空间，为空则查询所有命名空间"`
	LabelSelector string `json:"labelSelector" jsonschema:"description=标签选择器，用于过滤 Pod"`
}

// describePodInput describe_pod 工具输入参数
type describePodInput struct {
	Namespace string `json:"namespace" jsonschema:"description=Pod 所在命名空间，必填"`
	Name      string `json:"name" jsonschema:"description=Pod 名称，必填"`
}

// getPodLogsInput get_pod_logs 工具输入参数
type getPodLogsInput struct {
	Namespace string `json:"namespace" jsonschema:"description=Pod 所在命名空间，必填"`
	Name      string `json:"name" jsonschema:"description=Pod 名称，必填"`
	Container string `json:"container" jsonschema:"description=容器名称，为空则使用默认容器"`
	TailLines int    `json:"tailLines" jsonschema:"description=返回的日志行数，默认 100，最大 200"`
}

// listEventsInput list_events 工具输入参数
type listEventsInput struct {
	Namespace string `json:"namespace" jsonschema:"description=命名空间，为空则查询所有命名空间"`
}

// listDeploymentsInput list_deployments 工具输入参数
type listDeploymentsInput struct {
	Namespace string `json:"namespace" jsonschema:"description=命名空间，为空则查询所有命名空间"`
}

// listServicesInput list_services 工具输入参数
type listServicesInput struct {
	Namespace string `json:"namespace" jsonschema:"description=命名空间，为空则查询所有命名空间"`
}

// getNodesInput get_nodes 工具输入参数（无参数）
type getNodesInput struct{}

// listNamespacesInput list_namespaces 工具输入参数（无参数）
type listNamespacesInput struct{}

// executeSafeCommandInput execute_safe_command 工具输入参数
type executeSafeCommandInput struct {
	Command string `json:"command" jsonschema:"description=要执行的 Shell 命令，必填"`
	Reason  string `json:"reason" jsonschema:"description=执行此命令的原因，用于安全审计，必填"`
}

// buildTools 构建工具列表
func (r *ReActLLM) buildTools() ([]tool.InvokableTool, error) {
	var tools []tool.InvokableTool

	// list_pods 工具
	listPodsTool, err := utils.InferTool("list_pods", "列出 Pod 列表，支持按命名空间和标签选择器过滤", func(ctx context.Context, input listPodsInput) (string, error) {
		resp, err := r.gateway.ListPods(ctx, input.Namespace, input.LabelSelector)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create list_pods tool: %w", err)
	}
	tools = append(tools, listPodsTool)

	// describe_pod 工具
	describePodTool, err := utils.InferTool("describe_pod", "查看 Pod 详情，包括状态、事件、容器信息等", func(ctx context.Context, input describePodInput) (string, error) {
		resp, err := r.gateway.DescribePod(ctx, input.Namespace, input.Name)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create describe_pod tool: %w", err)
	}
	tools = append(tools, describePodTool)

	// get_pod_logs 工具
	getPodLogsTool, err := utils.InferTool("get_pod_logs", "获取 Pod 容器的日志输出", func(ctx context.Context, input getPodLogsInput) (string, error) {
		// 限制 tailLines 最大为 200
		tailLines := input.TailLines
		if tailLines <= 0 {
			tailLines = 100
		}
		if tailLines > 200 {
			tailLines = 200
		}
		resp, err := r.gateway.GetLogs(ctx, input.Namespace, input.Name, input.Container, tailLines)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create get_pod_logs tool: %w", err)
	}
	tools = append(tools, getPodLogsTool)

	// list_events 工具
	listEventsTool, err := utils.InferTool("list_events", "列出 Kubernetes 事件，可用于排查问题", func(ctx context.Context, input listEventsInput) (string, error) {
		resp, err := r.gateway.ListEvents(ctx, input.Namespace)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create list_events tool: %w", err)
	}
	tools = append(tools, listEventsTool)

	// list_deployments 工具
	listDeploymentsTool, err := utils.InferTool("list_deployments", "列出 Deployment 列表", func(ctx context.Context, input listDeploymentsInput) (string, error) {
		resp, err := r.gateway.ListDeployments(ctx, input.Namespace)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create list_deployments tool: %w", err)
	}
	tools = append(tools, listDeploymentsTool)

	// list_services 工具
	listServicesTool, err := utils.InferTool("list_services", "列出 Service 列表", func(ctx context.Context, input listServicesInput) (string, error) {
		resp, err := r.gateway.ListServices(ctx, input.Namespace)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create list_services tool: %w", err)
	}
	tools = append(tools, listServicesTool)

	// get_nodes 工具
	getNodesTool, err := utils.InferTool("get_nodes", "获取集群节点列表和状态", func(ctx context.Context, input getNodesInput) (string, error) {
		resp, err := r.gateway.GetNodes(ctx)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create get_nodes tool: %w", err)
	}
	tools = append(tools, getNodesTool)

	// list_namespaces 工具
	listNamespacesTool, err := utils.InferTool("list_namespaces", "列出集群中的所有命名空间", func(ctx context.Context, input listNamespacesInput) (string, error) {
		resp, err := r.gateway.ListNamespaces(ctx)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Stdout, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create list_namespaces tool: %w", err)
	}
	tools = append(tools, listNamespacesTool)

	// execute_safe_command 工具
	executeSafeCommandTool, err := utils.InferTool("execute_safe_command", "在集群节点上执行 Shell 命令（需通过安全审计）", func(ctx context.Context, input executeSafeCommandInput) (string, error) {
		// 优先走带回结果的路径
		if r.safeExecutorWithResult != nil && r.recorder != nil {
			result, err := r.safeExecutorWithResult.ExecuteSafeCommandWithResult(ctx, input.Command, input.Reason)
			if err != nil {
				return fmt.Sprintf("Error: %v", err), nil
			}
			allowed := result.AuditInfo != nil && result.AuditInfo.Allowed
			// emit 工具 trace（含 AuditInfo）
			r.recorder.Emit(trc.ToolExecutedEvent{Execution: trc.TraceToolExecution{
				ToolName:  "execute_safe_command",
				Args:      map[string]interface{}{"command": input.Command, "reason": input.Reason},
				Success:   allowed,
				Output:    result.Output,
				AuditInfo: trc.ConvertAuditInfo(result.AuditInfo),
				Timestamp: time.Now().Format(time.RFC3339),
			}})
			if !allowed {
				reason := "未知原因"
				advice := ""
				if result.AuditInfo != nil {
					reason = result.AuditInfo.Reason
					advice = result.AuditInfo.Advice
				}
				return fmt.Sprintf("命令被安全审计拒绝。原因: %s。建议: %s", reason, advice), nil
			}
			return result.Output, nil
		}
		// fallback：旧路径（无 recorder 或 executor 不支持扩展接口）
		output, err := r.safeExecutor.ExecuteSafeCommand(ctx, input.Command, input.Reason)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return output, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create execute_safe_command tool: %w", err)
	}
	tools = append(tools, executeSafeCommandTool)

	return tools, nil
}

// DeepQuery 执行深度调查（用于 ActionNode 的 deep_query 模式）
func (r *ReActLLM) DeepQuery(ctx context.Context, topic string, currentState *state.State) (string, *schema.TokenUsage, error) {
	logger.Info("starting ReAct deep query", logger.String("topic", topic))

	// 1. 获取 Power 模型
	powerModel := r.router.Power()
	if powerModel == nil {
		return "", nil, fmt.Errorf("power model not initialized")
	}

	// 尝试将模型转换为 ToolCallingChatModel
	toolCallingModel, ok := powerModel.(model.ToolCallingChatModel)
	if !ok {
		return "", nil, fmt.Errorf("power model does not support tool calling")
	}

	// 2. 构建工具列表
	tools, err := r.buildTools()
	if err != nil {
		return "", nil, fmt.Errorf("failed to build tools: %w", err)
	}

	// 转换工具为 BaseTool 接口
	baseTools := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		baseTools[i] = t
	}

	// 3. 创建 ReAct Agent
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: toolCallingModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: baseTools,
		},
		MaxStep: 10,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create ReAct agent: %w", err)
	}

	// 4. 构建系统提示词 + 用户查询
	systemMsg := schema.SystemMessage(r.GetSystemPrompt(ctx))

	// 构建集群状态摘要
	clusterSummary := "未获取"
	if currentState != nil && currentState.K8sInfo != nil {
		clusterSummary = currentState.K8sInfo.GetSummary()
	}

	userMsg := schema.UserMessage(fmt.Sprintf(
		"请针对以下问题进行深入调查：\n%s\n\n当前集群状态概况：\n%s",
		topic,
		clusterSummary,
	))

	// 5. 调用 agent 执行
	logger.Info("calling ReAct agent")
	option, future := react.WithMessageFuture()
	var wg sync.WaitGroup
	totalUsage := &schema.TokenUsage{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		iter := future.GetMessages()
		for {
			msg, ok, iterErr := iter.Next()
			if iterErr != nil || !ok {
				return
			}
			if msg == nil || msg.Role != schema.Assistant || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
				continue
			}
			usage := msg.ResponseMeta.Usage
			totalUsage.PromptTokens += usage.PromptTokens
			totalUsage.CompletionTokens += usage.CompletionTokens
			totalUsage.TotalTokens += usage.TotalTokens
			if r.recorder != nil {
				r.recorder.Emit(trc.LLMCallEvent{Call: trc.LLMCallRecord{
					ModelType:        "power",
					ModelName:        r.router.PowerModelName(),
					Source:           "deep_query",
					PromptTokens:     usage.PromptTokens,
					CompletionTokens: usage.CompletionTokens,
					TotalTokens:      usage.TotalTokens,
					DurationMs:       0, // 每轮无独立计时
					Timestamp:        time.Now().Format(time.RFC3339),
					Output:           msg.Content,
					CacheHit:         usage.PromptTokenDetails.CachedTokens > 0,
					CachedTokens:     usage.PromptTokenDetails.CachedTokens,
				}})
			}
		}
	}()
	response, err := agent.Generate(ctx, []*schema.Message{systemMsg, userMsg}, option)
	wg.Wait()
	if err != nil {
		return "", totalUsage, fmt.Errorf("ReAct agent execution failed: %w", err)
	}

	if response == nil {
		return "", totalUsage, fmt.Errorf("ReAct agent returned nil response")
	}

	logger.Info("ReAct deep query completed")
	return response.Content, totalUsage, nil
}
