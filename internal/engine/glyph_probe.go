package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *Store) GlyphProbe(sample, language, family string) (any, error) {
	if s.RomRoot != "" && s.RomRoot != "/" {
		return nil, fmt.Errorf("实际选字检查仅支持 Android 设备")
	}
	if len([]rune(sample)) == 0 || len([]rune(sample)) > 256 {
		return nil, fmt.Errorf("选字检查需要 1–256 个字符")
	}
	if language != "" && !validLanguage(language) {
		return nil, fmt.Errorf("语言标签无效")
	}
	if family == "" || family == "sans" {
		family = "sans-serif"
	}
	if !safeFamily(family) {
		return nil, fmt.Errorf("字体族名称无效")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/system/bin/app_process", "/system/bin", "local.hyperfont.FontRouteProbe", base64.StdEncoding.EncodeToString([]byte(sample)), language, family)
	jar := filepath.Join(filepath.Dir(exe), "font-route-probe.jar")
	if _, err := os.Stat(jar); os.IsNotExist(err) {
		jar = filepath.Join(filepath.Dir(exe), "..", "font-route-probe.jar")
	}
	if _, err := os.Stat(jar); err != nil {
		return nil, fmt.Errorf("缺少设备选字诊断工具: %w", err)
	}
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "CLASSPATH=") {
			command.Env = append(command.Env, env)
		}
	}
	command.Env = append(command.Env, "CLASSPATH="+jar)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("设备选字检查失败: %s", output)
	}
	var result any
	// app_process may emit VM warnings before the single JSON result line.
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 0 {
		output = []byte(lines[len(lines)-1])
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("设备选字检查响应无效: %s", output)
	}
	return result, nil
}
