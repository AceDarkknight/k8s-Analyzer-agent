package promptregistry

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptRegistryLoad_Success(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "blocks"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "safety"), 0o755))

	manifest := `version: "1.0.0"
blocks:
  - name: system_base
    file: blocks/system_base.md
    description: "base"
prompts:
  - name: safety
    version: default
    file: safety/default.md
    blocks: [system_base]
    is_active: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blocks", "system_base.md"), []byte("BASE"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "safety", "default.md"), []byte("CMD={{.Command}}"), 0o644))

	reg := NewPromptRegistry()
	err := reg.Load(dir)
	require.NoError(t, err)

	out, err := reg.BuildSafety(context.Background(), "safety", VersionDefault, &SafetyPromptContext{Command: "ls", Reason: "test"})
	require.NoError(t, err)
	assert.Contains(t, out, "BASE")
	assert.Contains(t, out, "CMD=ls")
}
