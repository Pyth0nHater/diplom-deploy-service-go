package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveNodeRuntimeUsesNvmrcFirst(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, ".nvmrc"), []byte("22\n"), 0644); err != nil {
		t.Fatalf("write .nvmrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"engines":{"node":">=20.9.0"}}`), 0644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	runtime, _, err := resolveNodeRuntime(workDir, DeployParams{})
	if err != nil {
		t.Fatalf("resolveNodeRuntime: %v", err)
	}
	if runtime.Image != "node:22-alpine" {
		t.Fatalf("unexpected node image: %s", runtime.Image)
	}
}

func TestResolveNodeRuntimeUsesEnginesConstraint(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"engines":{"node":">=20.9.0"}}`), 0644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	runtime, _, err := resolveNodeRuntime(workDir, DeployParams{})
	if err != nil {
		t.Fatalf("resolveNodeRuntime: %v", err)
	}
	if runtime.Image != "node:20.9.0-alpine" {
		t.Fatalf("unexpected node image: %s", runtime.Image)
	}
}

func TestResolveNodeRuntimeUsesRequestOverride(t *testing.T) {
	workDir := t.TempDir()

	runtime, _, err := resolveNodeRuntime(workDir, DeployParams{NodeVersion: "node:21-alpine"})
	if err != nil {
		t.Fatalf("resolveNodeRuntime: %v", err)
	}
	if runtime.Image != "node:21-alpine" {
		t.Fatalf("unexpected node image: %s", runtime.Image)
	}
}
