package parity

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type entry struct {
	label    string
	localDir string
}

type config struct {
	path    string
	entries []entry
}

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var pathVariablePattern = regexp.MustCompile(`\$\{(\w+)\}|\$(\w+)|^~`)

func parityDir() (string, error) {
	if dir := os.Getenv("PARITY_STATE_DIR"); dir != "" {
		return dir, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "parity"), nil
}

func expandPath(raw string) (string, error) {
	var expandErr error
	expanded := pathVariablePattern.ReplaceAllStringFunc(raw, func(match string) string {
		if match == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				expandErr = err
			}
			return home
		}
		parts := pathVariablePattern.FindStringSubmatch(match)
		name := parts[1]
		if name == "" {
			name = parts[2]
		}
		value, ok := os.LookupEnv(name)
		if !ok && expandErr == nil {
			expandErr = fmt.Errorf("environment variable %s is not set (in %q)", name, raw)
		}
		return value
	})
	return expanded, expandErr
}

func parseTOML(text, source string) ([]entry, error) {
	var parsed map[string]any
	metadata, err := toml.Decode(text, &parsed)
	if err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", source, err)
	}
	entries := make([]entry, 0, len(parsed))
	seen := make(map[string]bool)
	// The decoder's keys keep the order of labels in the config file.
	for _, key := range metadata.Keys() {
		if len(key) == 0 || seen[key[0]] {
			continue
		}
		label := key[0]
		seen[label] = true
		table, ok := parsed[label].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: [%s] must be a table with a local_dir string", source, label)
		}
		localDir, ok := table["local_dir"].(string)
		if !ok || strings.TrimSpace(localDir) == "" {
			return nil, fmt.Errorf("%s: [%s] is missing a local_dir string", source, label)
		}
		if !labelPattern.MatchString(label) {
			return nil, fmt.Errorf("%s: label %q must match %s", source, label, labelPattern)
		}
		expanded, err := expandPath(localDir)
		if err != nil {
			return nil, fmt.Errorf("%s: [%s]: %w", source, label, err)
		}
		entries = append(entries, entry{label: label, localDir: expanded})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: no entries defined", source)
	}
	return entries, nil
}

func loadConfig(flagPath, stateDir string) (config, error) {
	path := flagPath
	if path == "" {
		path = filepath.Join(stateDir, "parity.toml")
	}
	text, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return config{}, fmt.Errorf("config file not found: %s\ncreate it, or pass --config <path>. See parity-example.toml", path)
	}
	if err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	entries, err := parseTOML(string(text), path)
	return config{path: path, entries: entries}, err
}
