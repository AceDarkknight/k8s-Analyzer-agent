package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
)

// LLMRouter 管理 Light/Power 两个模型
type LLMRouter struct {
	light model.ChatModel
	power model.ChatModel
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
		light: lightModel,
		power: powerModel,
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

	return chatModel, nil
}

// Light 返回轻量模型
func (r *LLMRouter) Light() model.ChatModel {
	return r.light
}

// Power 返回强力模型
func (r *LLMRouter) Power() model.ChatModel {
	return r.power
}

// GenerateWithLight 使用轻量模型生成
func (r *LLMRouter) GenerateWithLight(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if r.light == nil {
		return nil, fmt.Errorf("light model not initialized")
	}
	return r.light.Generate(ctx, messages)
}

// GenerateWithPower 使用强力模型生成
func (r *LLMRouter) GenerateWithPower(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if r.power == nil {
		return nil, fmt.Errorf("power model not initialized")
	}
	return r.power.Generate(ctx, messages)
}
