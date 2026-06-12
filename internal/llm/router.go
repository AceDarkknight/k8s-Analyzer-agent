package llm

import (
	"context"
	"fmt"
	"net/http"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
)

// LLMRouter 管理 Light/Power 两个模型
type LLMRouter struct {
	light         model.ChatModel
	power         model.ChatModel
	lightModel    string
	powerModel    string
	lightTransport *CacheAwareTransport
	powerTransport *CacheAwareTransport
}

// NewLLMRouter 创建 LLM Router
func NewLLMRouter(ctx context.Context, cfg *config.AgentLLMConfig) (*LLMRouter, error) {
	// 创建轻量模型
	lightModel, lightTransport, err := createChatModel(ctx, &cfg.Light)
	if err != nil {
		return nil, fmt.Errorf("failed to create light model: %w", err)
	}

	// 创建强力模型
	powerModel, powerTransport, err := createChatModel(ctx, &cfg.Power)
	if err != nil {
		return nil, fmt.Errorf("failed to create power model: %w", err)
	}

	return &LLMRouter{
		light:         lightModel,
		power:         powerModel,
		lightModel:    cfg.Light.Model,
		powerModel:    cfg.Power.Model,
		lightTransport: lightTransport,
		powerTransport: powerTransport,
	}, nil
}

// createChatModel 根据配置创建 ChatModel
func createChatModel(ctx context.Context, cfg *config.LLMConfig) (model.ChatModel, *CacheAwareTransport, error) {
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

	// 创建缓存感知的 HTTP Client，用于拦截 API 响应并提取缓存命中信息
	// 不同的 LLM 提供商使用不同的字段名表示缓存命中：
	// - OpenAI: prompt_tokens_details.cached_tokens（标准格式）
	// - DeepSeek: prompt_cache_hit_tokens（自定义格式）
	// CacheAwareTransport 会自动检测并提取这些字段
	cacheAwareTransport := NewCacheAwareTransport(http.DefaultTransport)
	httpClient := &http.Client{Transport: cacheAwareTransport}

	chatModel, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Temperature: tempPtr,
		MaxTokens:   maxTokensPtr,
		HTTPClient:  httpClient,
	})
	if err != nil {
		return nil, nil, err
	}

	return chatModel, cacheAwareTransport, nil
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
	return msg, extractTokenUsage(msg, r.lightTransport), nil
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
	return msg, extractTokenUsage(msg, r.powerTransport), nil
}

// extractTokenUsage 从 Message 中提取 TokenUsage
// 不同的 LLM 提供商使用不同的字段名表示缓存命中：
// - OpenAI: prompt_tokens_details.cached_tokens（标准格式，eino-ext 会自动映射）
// - DeepSeek: prompt_cache_hit_tokens（自定义格式，需要通过 CacheAwareTransport 拦截）
//
// CacheAwareTransport 在 HTTP 层拦截响应并提取缓存命中信息，
// 每个 transport 实例维护自己的缓存信息，避免并发场景下的数据错配
func extractTokenUsage(msg *schema.Message, transport *CacheAwareTransport) *schema.TokenUsage {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return nil
	}
	usage := *msg.ResponseMeta.Usage

	// 如果 eino-ext 没有映射到缓存命中信息，尝试从对应 transport 中获取
	// 这主要针对 DeepSeek 等使用非标准字段名的提供商
	if usage.PromptTokenDetails.CachedTokens == 0 && transport != nil {
		if cacheInfo := transport.GetLastCacheInfo(); cacheInfo != nil {
			usage.PromptTokenDetails.CachedTokens = cacheInfo.PromptCacheHitTokens
		}
	}

	return &usage
}
