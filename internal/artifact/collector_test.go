package artifact

import "testing"

func TestSanitizePattern(t *testing.T) {
	if _, err := SanitizePattern("../etc/passwd"); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}
	if _, err := SanitizePattern("/abs/path"); err == nil {
		t.Fatal("绝对路径应被拒绝")
	}
	got, err := SanitizePattern(" dist/app.bin ")
	if err != nil || got != "dist/app.bin" {
		t.Fatalf("合法相对路径应通过: %q %v", got, err)
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
}
