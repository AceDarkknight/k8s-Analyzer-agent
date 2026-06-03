package llm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// CacheInfo 存储从 API 响应中提取的缓存命中信息
type CacheInfo struct {
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}

// CacheAwareTransport 是一个自定义的 HTTP RoundTripper，用于拦截 API 响应
// 它会读取响应体，提取缓存命中信息，然后重新构建响应体
// 每个 Transport 实例维护自己的缓存信息，避免并发场景下的数据错配
type CacheAwareTransport struct {
	delegate  http.RoundTripper
	mu        sync.RWMutex
	lastInfo  *CacheInfo
}

// NewCacheAwareTransport 创建一个缓存感知的 HTTP Transport
func NewCacheAwareTransport(delegate http.RoundTripper) *CacheAwareTransport {
	if delegate == nil {
		delegate = http.DefaultTransport
	}
	return &CacheAwareTransport{delegate: delegate}
}

// GetLastCacheInfo 获取最近一次 API 调用的缓存信息
func (t *CacheAwareTransport) GetLastCacheInfo() *CacheInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastInfo
}

// setLastCacheInfo 设置最近一次 API 调用的缓存信息
func (t *CacheAwareTransport) setLastCacheInfo(info *CacheInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastInfo = info
}

// RoundTrip 实现 http.RoundTripper 接口
func (t *CacheAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.delegate.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// 只拦截 JSON 响应，尝试提取缓存命中信息
	if resp != nil && resp.Body != nil {
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") {
			return t.interceptResponse(resp)
		}
	}

	return resp, nil
}

// interceptResponse 拦截 API 响应，提取缓存命中信息
func (t *CacheAwareTransport) interceptResponse(resp *http.Response) (*http.Response, error) {
	// 读取响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}
	resp.Body.Close()

	// 尝试解析响应体，提取缓存命中信息
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err == nil {
		if usage, ok := result["usage"].(map[string]interface{}); ok {
			cacheInfo := &CacheInfo{}

			// 尝试提取 prompt_cache_hit_tokens（DeepSeek 格式）
			if hitTokens, ok := usage["prompt_cache_hit_tokens"].(float64); ok {
				cacheInfo.PromptCacheHitTokens = int(hitTokens)
			}

			// 尝试提取 prompt_cache_miss_tokens（DeepSeek 格式）
			if missTokens, ok := usage["prompt_cache_miss_tokens"].(float64); ok {
				cacheInfo.PromptCacheMissTokens = int(missTokens)
			}

			// 如果 DeepSeek 格式没有数据，尝试 OpenAI 格式
			if cacheInfo.PromptCacheHitTokens == 0 {
				if promptDetails, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
					if cachedTokens, ok := promptDetails["cached_tokens"].(float64); ok {
						cacheInfo.PromptCacheHitTokens = int(cachedTokens)
					}
				}
			}

			if cacheInfo.PromptCacheHitTokens > 0 || cacheInfo.PromptCacheMissTokens > 0 {
				t.setLastCacheInfo(cacheInfo)
			}
		}
	}

	// 重新构建响应体
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	resp.ContentLength = int64(len(bodyBytes))

	return resp, nil
}
