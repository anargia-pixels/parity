package parity

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpandPath(t *testing.T) {
	t.Setenv("PARITY_TEST_DIR", "/tmp/parity-test")
	t.Setenv("HOME", "/tmp/parity-home")
	for raw, want := range map[string]string{
		"$PARITY_TEST_DIR/a":   "/tmp/parity-test/a",
		"${PARITY_TEST_DIR}/b": "/tmp/parity-test/b",
		"~/foo":                "/tmp/parity-home/foo",
		"/literal/path":        "/literal/path",
	} {
		got, err := expandPath(raw)
		if err != nil || got != want {
			t.Errorf("expandPath(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := expandPath("$PARITY_UNSET_VAR_XYZ"); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("missing variable should fail: %v", err)
	}
}

func TestParseTOML(t *testing.T) {
	t.Setenv("PARITY_TEST_DIR", "/tmp/parity-test")
	entries, err := parseTOML("[z-editor]\nlocal_dir = '$PARITY_TEST_DIR/editor'\n[a-terminal]\nlocal_dir = '/tmp/terminal'\n", "test.toml")
	want := []entry{{"z-editor", "/tmp/parity-test/editor"}, {"a-terminal", "/tmp/terminal"}}
	if err != nil || !reflect.DeepEqual(entries, want) {
		t.Fatalf("parseTOML = %#v, %v; want %#v", entries, err, want)
	}
	dotted, err := parseTOML("opencode.local_dir = '/tmp/opencode'", "test.toml")
	if err != nil || !reflect.DeepEqual(dotted, []entry{{"opencode", "/tmp/opencode"}}) {
		t.Fatalf("dotted TOML keys: %#v, %v", dotted, err)
	}
	for _, test := range []struct{ name, text, message string }{
		{"missing path", "[opencode]\nfoo = 1", "missing a local_dir"},
		{"blank path", "[opencode]\nlocal_dir = '  '", "missing a local_dir"},
		{"invalid label", "['bad label']\nlocal_dir = '/x'", "must match"},
		{"empty", "", "no entries"},
		{"invalid TOML", "a = 1\na = 2", "could not parse"},
		{"scalar", "opencode = 1", "must be a table"},
		{"array", "[[opencode]]\nlocal_dir = '/x'", "must be a table"},
		{"missing variable", "[opencode]\nlocal_dir = '$PARITY_UNSET_VAR_XYZ'", "not set"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseTOML(test.text, "test.toml"); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
}

func TestConfigLocations(t *testing.T) {
	t.Setenv("PARITY_STATE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "parity")
	if got, err := parityDir(); err != nil || got != want {
		t.Fatalf("parityDir = %q, %v; want %q", got, err, want)
	}
	t.Setenv("PARITY_STATE_DIR", t.TempDir())
	stateDir, err := parityDir()
	if err != nil || stateDir != os.Getenv("PARITY_STATE_DIR") {
		t.Fatalf("state override: %q, %v", stateDir, err)
	}
	path := filepath.Join(stateDir, "parity.toml")
	writeTestFile(t, path, "[editor]\nlocal_dir = '/tmp/editor'\n")
	if cfg, err := loadConfig("", stateDir); err != nil || cfg.path != path || len(cfg.entries) != 1 {
		t.Fatalf("default config: %#v, %v", cfg, err)
	}
	custom := filepath.Join(t.TempDir(), "custom.toml")
	writeTestFile(t, custom, "[terminal]\nlocal_dir = '/tmp/terminal'\n")
	if cfg, err := loadConfig(custom, stateDir); err != nil || cfg.entries[0].label != "terminal" {
		t.Fatalf("custom config: %#v, %v", cfg, err)
	}
	if _, err := loadConfig(filepath.Join(stateDir, "missing.toml"), stateDir); err == nil || !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("missing config: %v", err)
	}
}
