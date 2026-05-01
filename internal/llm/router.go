package llm

import (
	"context"
	"encoding/json"
	"fmt"

	openaiacl "github.com/cloudwego/eino-ext/libs/acl/openai"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
)

// LLMRouter 管理 Light/Power 两个模型
type LLMRouter struct {
	light       model.ChatModel
	power       model.ChatModel
	lightModel  string
	powerModel  string
}

// NewLLMRouter 创建 LLM Router
func NewLLMRouter(ctx context.Context, cfg *config.AgentLLMConfig) (*LLMRouter, error) {
	// 创建轻量模型
	lightModel, err := createChatModel(ctx, &cfg.Light)
	if err != nil {
		return nil, fmt.Errorf("failed to create light model: %w", err)
	}

	// 创建强力模型
	powerModel, err := createChatModel(ctx, &cfg.Power)
	if err != nil {
		return nil, fmt.Errorf("failed to create power model: %w", err)
	}

	return &LLMRouter{
		light:      lightModel,
		power:      powerModel,
		lightModel: cfg.Light.Model,
		powerModel: cfg.Power.Model,
	}, nil
}

// createChatModel 根据配置创建 ChatModel
func createChatModel(ctx context.Context, cfg *config.LLMConfig) (model.ChatModel, error) {
	// 转换温度值为 float32 指针
	var tempPtr *float32
	if cfg.Temperature >= 0 {
		temp := float32(cfg.Temperature)
		tempPtr = &temp
	}

	// 转换 MaxTokens 为 int 指针
	var maxTokensPtr *int
	if cfg.MaxTokens > 0 {
		maxTokensPtr = &cfg.MaxTokens
	}

	chatModel, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Temperature: tempPtr,
		MaxTokens:   maxTokensPtr,
	})
	if err != nil {
		return nil, err
	}

	return &cachedChatModel{inner: chatModel}, nil
}

// Light 返回轻量模型
func (r *LLMRouter) Light() model.ChatModel {
	return r.light
}

// Power 返回强力模型
func (r *LLMRouter) Power() model.ChatModel {
	return r.power
}

// LightModelName 返回轻量模型名称
func (r *LLMRouter) LightModelName() string {
	return r.lightModel
}

// PowerModelName 返回强力模型名称
func (r *LLMRouter) PowerModelName() string {
	return r.powerModel
}

// GenerateWithLight 使用轻量模型生成
func (r *LLMRouter) GenerateWithLight(ctx context.Context, messages []*schema.Message) (*schema.Message, *schema.TokenUsage, error) {
	if r.light == nil {
		return nil, nil, fmt.Errorf("light model not initialized")
	}
	msg, err := r.light.Generate(ctx, messages)
	if err != nil {
		return nil, nil, err
	}
	return msg, extractTokenUsage(msg), nil
}

// GenerateWithPower 使用强力模型生成
func (r *LLMRouter) GenerateWithPower(ctx context.Context, messages []*schema.Message) (*schema.Message, *schema.TokenUsage, error) {
	if r.power == nil {
		return nil, nil, fmt.Errorf("power model not initialized")
	}
	msg, err := r.power.Generate(ctx, messages)
	if err != nil {
		return nil, nil, err
	}
	return msg, extractTokenUsage(msg), nil
}

func extractTokenUsage(msg *schema.Message) *schema.TokenUsage {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return nil
	}
	usage := *msg.ResponseMeta.Usage
	return &usage
}


type cachedChatModel struct {
	inner model.ChatModel
}

func (c *cachedChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	opts = append(opts, openaiacl.WithResponseMessageModifier(cacheTokensModifier))
	return c.inner.Generate(ctx, input, opts...)
}

func (c *cachedChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	opts = append(opts, openaiacl.WithResponseMessageModifier(cacheTokensModifier))
	return c.inner.Stream(ctx, input, opts...)
}

func (c *cachedChatModel) BindTools(tools []*schema.ToolInfo) error {
	return c.inner.BindTools(tools)
}

func (c *cachedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	tc, ok := c.inner.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("chat model does not support tool calling")
	}
	wrapped, err := tc.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &cachedToolCallingChatModel{inner: wrapped}, nil
}

type cachedToolCallingChatModel struct {
	inner model.ToolCallingChatModel
}

func (c *cachedToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	opts = append(opts, openaiacl.WithResponseMessageModifier(cacheTokensModifier))
	return c.inner.Generate(ctx, input, opts...)
}

func (c *cachedToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	opts = append(opts, openaiacl.WithResponseMessageModifier(cacheTokensModifier))
	return c.inner.Stream(ctx, input, opts...)
}

func (c *cachedToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	wrapped, err := c.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &cachedToolCallingChatModel{inner: wrapped}, nil
}

func cacheTokensModifier(_ context.Context, msg *schema.Message, rawBody []byte) (*schema.Message, error) {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return msg, nil
	}

	if msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens > 0 {
		return msg, nil
	}

	var raw struct {
		Usage struct {
			PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
			PromptTokensDetails  *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return msg, nil
	}

	switch {
	case raw.Usage.PromptCacheHitTokens > 0:
		msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens = raw.Usage.PromptCacheHitTokens
	case raw.Usage.PromptTokensDetails != nil && raw.Usage.PromptTokensDetails.CachedTokens > 0:
		msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens = raw.Usage.PromptTokensDetails.CachedTokens
	}

	return msg, nil
}
