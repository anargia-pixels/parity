package parity

import (
	"slices"
	"testing"
)

func TestFindSecretsInDiff(t *testing.T) {
	for _, test := range []struct{ line, pattern string }{
		{`+api_key = "abcdefghijklmnopqrstuvwxyz123"`, "api key assignment"},
		{`+sk-1234567890abcdefghijklmnop`, "openai api key"},
		{`+ghp_1234567890abcdefghijklmnopqrstuv`, "github token"},
		{`+xoxb-1234567890abcdefgh`, "slack token"},
		{`+AKIA1234567890ABCDEF`, "aws access key"},
		{`+-----BEGIN OPENSSH PRIVATE KEY-----`, "private key"},
	} {
		if got := findSecretsInDiff(test.line); !slices.Contains(got, test.pattern) {
			t.Errorf("%q: expected %q, got %v", test.line, test.pattern, got)
		}
	}
	if got := findSecretsInDiff("+++ b/sk-1234567890abcdefghijklmnop\n context\n-removed\n+plain text"); len(got) != 0 {
		t.Fatalf("headers and unchanged text must be ignored: %v", got)
	}
}
