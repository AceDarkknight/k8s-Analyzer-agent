// Package client 提供配置加载与管理功能
package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadConfig 从 JSON 文件加载配置到目标结构体
func LoadConfig[T any](configPath string) (*T, error) {
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析 JSON
	var config T
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// LoadConfigWithEnv 从 JSON 文件加载配置，并支持环境变量覆盖
// 支持的环境变量格式：MCP_<SECTION>_<KEY> 或 MCP_<KEY>
func LoadConfigWithEnv[T any](configPath string) (*T, error) {
	config, err := LoadConfig[T](configPath)
	if err != nil {
		return nil, err
	}

	// 将配置转换为 map 以便处理环境变量覆盖
	configMap, err := structToMap(config)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config to map: %w", err)
	}

	// 遍历环境变量并覆盖配置
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "MCP_") {
			continue
		}

		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		envKey := parts[0][4:] // 移除 "MCP_" 前缀
		envValue := parts[1]

		// 支持两种格式：
		// 1. MCP_SECTION_KEY -> config["section"]["key"]
		// 2. MCP_KEY -> config["key"]
		if strings.Contains(envKey, "_") {
			sectionAndKey := strings.SplitN(envKey, "_", 2)
			if len(sectionAndKey) == 2 {
				section := strings.ToLower(sectionAndKey[0])
				key := strings.ToLower(sectionAndKey[1])
				if sectionMap, ok := configMap[section].(map[string]interface{}); ok {
					sectionMap[key] = envValue
				}
			}
		} else {
			key := strings.ToLower(envKey)
			configMap[key] = envValue
		}
	}

	// 将 map 转换回结构体
	result, err := mapToStruct[T](configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert map to config: %w", err)
	}

	return result, nil
}

// structToMap 将结构体转换为 map
func structToMap(v interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// mapToStruct 将 map 转换为结构体
func mapToStruct[T any](m map[string]interface{}) (*T, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetConfigPath 获取配置文件的绝对路径
// 支持相对路径和绝对路径
func GetConfigPath(configPath string) (string, error) {
	// 如果是绝对路径，直接返回
	if filepath.IsAbs(configPath) {
		return configPath, nil
	}

	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	// 拼接绝对路径
	absPath := filepath.Join(cwd, configPath)

	// 检查文件是否存在
	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("config file not found: %s", absPath)
	}

	return absPath, nil
}

// ValidateConfig 验证配置是否有效
// 需要配置结构体实现 Validate() 方法
type ConfigValidator interface {
	Validate() error
}

// Validate 验证配置
func Validate(config ConfigValidator) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	return config.Validate()
}

// SaveConfig 将配置保存到文件
func SaveConfig[T any](config *T, configPath string) error {
	// 确保目录存在
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 序列化配置
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// MergeConfig 合并两个配置，第二个配置的值会覆盖第一个配置
func MergeConfig[T any](base, override *T) (*T, error) {
	if base == nil {
		return override, nil
	}
	if override == nil {
		return base, nil
	}

	// 将两个配置都转换为 map
	baseMap, err := structToMap(base)
	if err != nil {
		return nil, fmt.Errorf("failed to convert base config: %w", err)
	}

	overrideMap, err := structToMap(override)
	if err != nil {
		return nil, fmt.Errorf("failed to convert override config: %w", err)
	}

	// 递归合并
	merged := mergeMaps(baseMap, overrideMap)

	// 转换回结构体
	result, err := mapToStruct[T](merged)
	if err != nil {
		return nil, fmt.Errorf("failed to convert merged config: %w", err)
	}

	return result, nil
}

// mergeMaps 递归合并两个 map
func mergeMaps(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// 复制 base 的所有值
	for k, v := range base {
		result[k] = v
	}

	// 用 override 的值覆盖
	for k, v := range override {
		if baseVal, exists := base[k]; exists {
			// 如果都是 map，递归合并
			if baseMap, ok := baseVal.(map[string]interface{}); ok {
				if overrideMap, ok := v.(map[string]interface{}); ok {
					result[k] = mergeMaps(baseMap, overrideMap)
					continue
				}
			}
		}
		result[k] = v
	}

	return result
}
