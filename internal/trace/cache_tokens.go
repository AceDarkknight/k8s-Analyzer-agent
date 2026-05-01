package trace

import "github.com/cloudwego/eino/schema"

// ExtractCachedTokens returns cached prompt tokens from the normalized usage data.
func ExtractCachedTokens(usage *schema.TokenUsage) int {
	if usage == nil {
		return 0
	}

	return usage.PromptTokenDetails.CachedTokens
}
