package config

import (
	"flag"
	"os"
	"testing"
)

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("MINIDRONE_ADDR", ":9090")
	t.Setenv("MINIDRONE_WORKERS", "6")
	t.Setenv("MINIDRONE_LOG_LEVEL", "debug")
	c := FromEnv()
	if c.Addr != ":9090" || c.Workers != 6 || !c.Verbose {
		t.Fatalf("环境变量未生效: %+v", c)
	}
}

func TestValidateFixesInvalid(t *testing.T) {
	c := Config{Workers: -1, MaxParallelStages: 0, Addr: ""}
	c.Validate()
	if c.Workers != 4 || c.MaxParallelStages != 8 || c.Addr != ":8080" {
		t.Fatalf("校验未回落默认: %+v", c)
	}
}

func TestRegisterFlags(t *testing.T) {
	c := Defaults()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c.RegisterFlags(fs)
	if err := fs.Parse([]string{"-addr", ":1", "-workers", "2"}); err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":1" || c.Workers != 2 {
		t.Fatalf("flag 未覆盖: %+v", c)
	}
}

func TestDefaults(t *testing.T) {
	_ = os.Unsetenv("MINIDRONE_ADDR")
	c := Defaults()
	if c.PipelinesDir != "pipelines" || c.ArtifactDir != "artifacts" {
		t.Fatalf("默认目录不符: %+v", c)
	}
}
