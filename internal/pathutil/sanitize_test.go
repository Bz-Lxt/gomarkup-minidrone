package pathutil

import "testing"

func TestSanitizePattern(t *testing.T) {
	bad := []string{
		"../etc/passwd",
		"/abs/path",
		`..\etc\passwd`,
		"reports/../../secrets.txt",
		"a/../../../etc/shadow",
		"dist/../../../secret",
		"..",
		"./../secret",
	}
	for _, p := range bad {
		if _, err := SanitizePattern(p); err == nil {
			t.Errorf("路径穿越应被拒绝: %q", p)
		}
	}
	good := []struct{ in, want string }{
		{" dist/app.bin ", "dist/app.bin"},
		{"dist/*.tgz", "dist/*.tgz"},
		{"reports/report.html", "reports/report.html"},
		{"./build/out", "./build/out"},
		{"a/b/c.txt", "a/b/c.txt"},
		{"dist/./pkg", "dist/./pkg"},
	}
	for _, c := range good {
		got, err := SanitizePattern(c.in)
		if err != nil || got != c.want {
			t.Errorf("合法相对路径应通过: %q -> got %q err %v", c.in, got, err)
		}
	}
}

func TestFilter(t *testing.T) {
	out, err := Filter([]string{"a.bin", "out/*.tgz"})
	if err != nil || len(out) != 2 {
		t.Fatalf("got %v %v", out, err)
	}
	if _, err := Filter([]string{"ok", ".."}); err == nil {
		t.Fatal("含穿越的列表应失败")
	}
	if _, err := Filter([]string{"ok", "reports/../../secrets.txt"}); err == nil {
		t.Fatal("嵌套穿越的列表应失败")
	}
}
