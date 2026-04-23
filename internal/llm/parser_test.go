package llm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "pure json",
			input:    `{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
		{
			name:     "json code block",
			input:    "```json\n{\"key\":\"value\"}\n```",
			expected: `{"key":"value"}`,
		},
		{
			name:     "embedded json",
			input:    "before {\"key\":\"value\"} after",
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractJSON(tt.input); got != tt.expected {
				t.Fatalf("ExtractJSON() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseDecisionResponse(t *testing.T) {
	t.Run("json continue converts to execute_plan", func(t *testing.T) {
		content := `{
			"thought": "check logs",
			"decision": "continue",
			"tool_calls": [{"name":"get_pod_logs","args":{"namespace":"default","name":"pod-1"}}]
		}`

		res, err := ParseDecisionResponse(content)
		if err != nil {
			t.Fatalf("ParseDecisionResponse returned error: %v", err)
		}
		if res.Decision != "execute_plan" {
			t.Fatalf("expected decision execute_plan, got %q", res.Decision)
		}
		if len(res.Plan) != 1 || len(res.Plan[0].ToolCalls) != 1 {
			t.Fatalf("expected converted plan with one tool call, got %+v", res.Plan)
		}
	})

	t.Run("fallback report keyword", func(t *testing.T) {
		res, err := ParseDecisionResponse("this is not json but decision: report")
		if err != nil {
			t.Fatalf("ParseDecisionResponse fallback returned error: %v", err)
		}
		if res.Decision != "report" {
			t.Fatalf("expected decision report, got %q", res.Decision)
		}
	})

	t.Run("empty content errors", func(t *testing.T) {
		if _, err := ParseDecisionResponse(""); err == nil {
			t.Fatal("expected error for empty content")
		}
	})
}

func TestParseAuditResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		res, err := ParseAuditResponse(`{"safety_level":"safe","reason":"readonly","advice":""}`)
		if err != nil {
			t.Fatalf("ParseAuditResponse returned error: %v", err)
		}
		if res.SafetyLevel != "safe" || res.Reason != "readonly" {
			t.Fatalf("unexpected audit response: %+v", res)
		}
	})

	t.Run("invalid level errors", func(t *testing.T) {
		if _, err := ParseAuditResponse(`{"safety_level":"invalid","reason":"x"}`); err == nil {
			t.Fatal("expected error for invalid safety level")
		}
	})
}

func TestParseAnalysisResponse(t *testing.T) {
	t.Run("sets non-zero finding timestamp", func(t *testing.T) {
		content := `{
			"summary": "test summary",
			"severity": "info",
			"root_cause": "none",
			"findings": [
				{
					"resource": "default/pod-1",
					"severity": "warning",
					"message": "test finding",
					"evidence": "evidence text"
				}
			],
			"recommendations": [],
			"limitations": ""
		}`

		before := time.Now()
		res, err := ParseAnalysisResponse(content)
		if err != nil {
			t.Fatalf("ParseAnalysisResponse returned error: %v", err)
		}

		if len(res.Findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(res.Findings))
		}

		finding := res.Findings[0]
		if finding.Timestamp.IsZero() {
			t.Fatalf("expected non-zero Timestamp on Finding")
		}
		if finding.Timestamp.Before(before) {
			t.Fatalf("expected Timestamp to be assigned during parsing, got %s before %s", finding.Timestamp, before)
		}

		data, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal analysis result: %v", err)
		}
		if strings.Contains(string(data), "0001-01-01T00:00:00Z") {
			t.Fatalf("expected marshaled result to exclude zero timestamp, got %s", string(data))
		}
	})

	t.Run("invalid json errors", func(t *testing.T) {
		if _, err := ParseAnalysisResponse("not json"); err == nil {
			t.Fatal("expected error for invalid analysis json")
		}
	})
}
