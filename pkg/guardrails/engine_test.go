package guardrails

import "testing"

func TestCommandChecker_AllowDeny(t *testing.T) {
	checker := NewCommandChecker(CommandRules{
		Allow: []string{"git *", "npm *"},
		Deny:  []string{"sudo *", "rm -rf /"},
	})

	cases := []struct {
		cmd     string
		allowed bool
	}{
		{"git status", true},
		{"npm install", true},
		{"sudo ls", false},
		{"rm -rf /", false},
		{"curl example.com", false},
	}

	for _, tc := range cases {
		allowed, _ := checker.Check(tc.cmd)
		if allowed != tc.allowed {
			t.Errorf("%q: expected allowed=%v, got %v", tc.cmd, tc.allowed, allowed)
		}
	}
}

func TestFilesystemChecker_ReadWrite(t *testing.T) {
	checker := NewFilesystemChecker(FilesystemRules{
		WritablePaths: []string{"/workspace"},
		ReadablePaths: []string{"/usr/local/bin"},
		DeniedPaths:   []string{"/etc/shadow"},
	})

	cases := []struct {
		path    string
		op      FileOp
		allowed bool
	}{
		{"/workspace/file.txt", FileOpWrite, true},
		{"/workspace/file.txt", FileOpRead, true},
		{"/usr/local/bin/foo", FileOpRead, true},
		{"/usr/local/bin/foo", FileOpWrite, false},
		{"/etc/shadow", FileOpRead, false},
	}

	for _, tc := range cases {
		allowed, _ := checker.Check(tc.path, tc.op)
		if allowed != tc.allowed {
			t.Errorf("%s %s: expected allowed=%v, got %v", tc.path, tc.op, tc.allowed, allowed)
		}
	}
}
