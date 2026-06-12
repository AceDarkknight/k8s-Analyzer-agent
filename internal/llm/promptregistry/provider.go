package promptregistry

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type PromptManifest struct {
	Version string      `yaml:"version"`
	Blocks  []BlockDef  `yaml:"blocks"`
	Prompts []PromptDef `yaml:"prompts"`
}

type BlockDef struct {
	Name        string `yaml:"name"`
	File        string `yaml:"file"`
	Description string `yaml:"description"`
}

type PromptDef struct {
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Label        string   `yaml:"label"`
	File         string   `yaml:"file"`
	SystemPrompt string   `yaml:"system_prompt"`
	Blocks       []string `yaml:"blocks"`
	Role         string   `yaml:"role"`
	ModelHint    string   `yaml:"model_hint"`
	Environment  string   `yaml:"environment"`
	Description  string   `yaml:"description"`
	Tags         []string `yaml:"tags"`
	IsActive     bool     `yaml:"is_active"`
}

// Load 从指定目录加载所有 prompt 和 block。
func (r *PromptRegistry) Load(promptsDir string) error {
	if !filepath.IsAbs(promptsDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		promptsDir = filepath.Join(cwd, promptsDir)
	}

	if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
		return fmt.Errorf("prompts directory not found: %s", promptsDir)
	} else if err != nil {
		return fmt.Errorf("stat prompts directory %s: %w", promptsDir, err)
	}

	manifestPath := filepath.Join(promptsDir, "registry.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", manifestPath, err)
	}

	var manifest PromptManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	for _, block := range manifest.Blocks {
		blockPath := filepath.Join(promptsDir, block.File)
		content, readErr := os.ReadFile(blockPath)
		if readErr != nil {
			return fmt.Errorf("read block %s (%s): %w", block.Name, blockPath, readErr)
		}

		if err := r.RegisterBlock(PromptSpec{
			Name:     PromptID(block.Name),
			Template: string(content),
			Metadata: PromptMetadata{Description: block.Description},
		}); err != nil {
			return fmt.Errorf("register block %s: %w", block.Name, err)
		}
	}

	for _, prompt := range manifest.Prompts {
		promptPath := filepath.Join(promptsDir, prompt.File)
		tmpl, readErr := os.ReadFile(promptPath)
		if readErr != nil {
			return fmt.Errorf("read prompt %s (%s): %w", prompt.Name, promptPath, readErr)
		}

		refs := make([]PromptID, 0, len(prompt.Blocks))
		for _, b := range prompt.Blocks {
			refs = append(refs, PromptID(b))
		}

		role := RoleUser
		if prompt.Role != "" {
			role = PromptRole(prompt.Role)
		}

		if err := r.Register(PromptSpec{
			Name:         PromptID(prompt.Name),
			Version:      PromptVersion(prompt.Version),
			Label:        prompt.Label,
			Template:     string(tmpl),
			SystemPrompt: prompt.SystemPrompt,
			Blocks:       refs,
			Role:         role,
			ModelHint:    prompt.ModelHint,
			IsActive:     prompt.IsActive,
			Metadata: PromptMetadata{
				Description: prompt.Description,
				Environment: prompt.Environment,
				Tags:        prompt.Tags,
			},
		}); err != nil {
			return fmt.Errorf("register prompt %s: %w", prompt.Name, err)
		}
	}

	return nil
}
