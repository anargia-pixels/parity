package parity

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type secretFinding struct {
	file    string
	pattern string
}

var secretPatterns = []struct {
	name  string
	regex *regexp.Regexp
}{
	{"openai api key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`)},
	{"github token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`)},
	{"slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"aws access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"api key assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|api[_-]?token|access[_-]?token|auth[_-]?token|refresh[_-]?token|client[_-]?secret|secret[_-]?key|password|passwd)\s*[:=]\s*["']?[A-Za-z0-9+/_.=-]{12,}["']?`)},
}

func findSecretsInDiff(diff string) []string {
	var findings []string
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		for _, pattern := range secretPatterns {
			if pattern.regex.MatchString(line) {
				findings = append(findings, pattern.name)
			}
		}
	}
	return findings
}

func stagedFiles(ctx context.Context, dir string) ([]string, error) {
	names := git(ctx, dir, "diff", "--cached", "--name-only", "--no-renames", "-z")
	if names.code != 0 {
		return nil, fmt.Errorf("list staged files: %s", names.stderr)
	}
	numstat := git(ctx, dir, "diff", "--cached", "--numstat", "--no-renames", "-z")
	if numstat.code != 0 {
		return nil, fmt.Errorf("check staged file types: %s", numstat.stderr)
	}
	binary := make(map[string]bool)
	for _, stat := range strings.Split(numstat.stdout, "\x00") {
		parts := strings.SplitN(stat, "\t", 3)
		if len(parts) == 3 && parts[0] == "-" && parts[1] == "-" {
			binary[parts[2]] = true
		}
	}
	var files []string
	for _, file := range strings.Split(names.stdout, "\x00") {
		if file != "" && !binary[file] {
			files = append(files, file)
		}
	}
	return files, nil
}

func checkStagedForSecrets(ctx context.Context, dir string) ([]secretFinding, error) {
	files, err := stagedFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	var findings []secretFinding
	for _, file := range files {
		diff := git(ctx, dir, "--literal-pathspecs", "diff", "--cached", "--no-ext-diff", "--no-textconv", "--", file)
		if diff.code != 0 {
			return nil, fmt.Errorf("check staged secrets in %q: %s", file, diff.stderr)
		}
		for _, pattern := range findSecretsInDiff(diff.stdout) {
			findings = append(findings, secretFinding{file, pattern})
		}
	}
	return findings, nil
}
