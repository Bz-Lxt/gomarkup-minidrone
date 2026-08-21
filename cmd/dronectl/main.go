// dronectl 是 MiniDrone 的命令行客户端，用于查看与触发流水线。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func baseURL() string {
	if v := os.Getenv("MINIDRONE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "health":
		err = cmdHealth()
	case "pipelines":
		err = cmdPipelines()
	case "run":
		err = cmdRun(os.Args[2:])
	case "builds":
		err = cmdBuilds()
	case "show":
		err = cmdShow(os.Args[2:])
	case "logs":
		err = cmdLogs(os.Args[2:])
	case "cancel":
		err = cmdCancel(os.Args[2:])
	case "metrics":
		err = cmdMetrics()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `dronectl — MiniDrone 命令行客户端

用法:
  dronectl health
  dronectl pipelines
  dronectl run <pipeline> [--repo URL] [--branch NAME]
  dronectl builds
  dronectl show <build-id>
  dronectl logs <build-id> <stage> <step>
  dronectl cancel <build-id>
  dronectl metrics

环境变量 MINIDRONE_URL 指定服务地址，默认 http://localhost:8080
`)
}

func get(path string) ([]byte, error) {
	resp, err := http.Get(baseURL() + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func post(path string, payload any) ([]byte, error) {
	var rdr io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	resp, err := http.Post(baseURL()+path, "application/json", rdr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func cmdHealth() error {
	b, err := get("/api/health")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func cmdPipelines() error {
	b, err := get("/api/pipelines")
	if err != nil {
		return err
	}
	var items []struct {
		Name   string `json:"name"`
		Stages []struct {
			Name  string `json:"name"`
			Steps []any  `json:"steps"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(b, &items); err != nil {
		return err
	}
	for _, p := range items {
		fmt.Printf("%s  (%d stages)\n", p.Name, len(p.Stages))
		for _, st := range p.Stages {
			fmt.Printf("  - %s  %d steps\n", st.Name, len(st.Steps))
		}
	}
	return nil
}

func cmdRun(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: dronectl run <pipeline> [--repo URL] [--branch NAME]")
	}
	name := args[0]
	body := map[string]string{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			i++
			if i < len(args) {
				body["repo"] = args[i]
			}
		case "--branch":
			i++
			if i < len(args) {
				body["branch"] = args[i]
			}
		}
	}
	raw, err := post("/api/pipelines/"+name+"/run", body)
	if err != nil {
		return err
	}
	var b struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	fmt.Printf("已触发 %s #%d  id=%s  state=%s\n", name, b.Number, b.ID, b.State)
	return nil
}

func cmdBuilds() error {
	raw, err := get("/api/builds")
	if err != nil {
		return err
	}
	var items []struct {
		ID        string    `json:"id"`
		Pipeline  string    `json:"pipeline"`
		Number    int       `json:"number"`
		State     string    `json:"state"`
		Trigger   string    `json:"trigger"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, b := range items {
		fmt.Printf("%s  %s #%d  %-10s  %-14s  %s\n",
			b.ID, b.Pipeline, b.Number, b.State, b.Trigger, b.CreatedAt.Format("15:04:05"))
	}
	return nil
}

func cmdShow(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: dronectl show <build-id>")
	}
	raw, err := get("/api/builds/" + args[0])
	if err != nil {
		return err
	}
	var b struct {
		Pipeline string `json:"pipeline"`
		Number   int    `json:"number"`
		State    string `json:"state"`
		Stages   []struct {
			Name  string `json:"name"`
			State string `json:"state"`
			Steps []struct {
				Name     string `json:"name"`
				State    string `json:"state"`
				ExitCode int    `json:"exit_code"`
			} `json:"steps"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	fmt.Printf("%s #%d  %s\n", b.Pipeline, b.Number, b.State)
	for _, st := range b.Stages {
		fmt.Printf("  [%s] %s\n", st.State, st.Name)
		for _, sp := range st.Steps {
			fmt.Printf("      %s  %s  exit=%d\n", sp.Name, sp.State, sp.ExitCode)
		}
	}
	return nil
}

func cmdLogs(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("用法: dronectl logs <build-id> <stage> <step>")
	}
	raw, err := get(fmt.Sprintf("/api/builds/%s/logs?stage=%s&step=%s", args[0], args[1], args[2]))
	if err != nil {
		return err
	}
	os.Stdout.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

func cmdCancel(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: dronectl cancel <build-id>")
	}
	_, err := post("/api/builds/"+args[0]+"/cancel", nil)
	if err != nil {
		return err
	}
	fmt.Println("已请求取消")
	return nil
}

func cmdMetrics() error {
	raw, err := get("/api/metrics")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}
