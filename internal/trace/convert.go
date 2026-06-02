package trace

import "reflect"

// ConvertAuditInfo 将审计信息转换为 trace 层的 TraceAuditInfo。
// 返回 nil 表示无审计信息。
func ConvertAuditInfo(info any) *TraceAuditInfo {
	if info == nil {
		return nil
	}

	value := reflect.ValueOf(info)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}

	return &TraceAuditInfo{
		Allowed:     boolField(value, "Allowed"),
		SafetyLevel: stringField(value, "SafetyLevel"),
		Reason:      stringField(value, "Reason"),
		Advice:      stringField(value, "Advice"),
		Method:      stringField(value, "Method"),
	}
}

func boolField(value reflect.Value, name string) bool {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false
	}
	return field.Bool()
}

func stringField(value reflect.Value, name string) string {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

// SanitizeTraceText 对写入 trace 的文本进行脱敏。
// 复用 sanitizer.go 中已有的 sensitivePatterns。
func SanitizeTraceText(text string) string {
	return sanitizeString(text)
}
