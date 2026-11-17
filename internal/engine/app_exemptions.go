package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const exemptionLimit = 128

var packageNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func validPackage(name string) bool { return len(name) <= 160 && packageNamePattern.MatchString(name) }

func validateExemptions(packages []string) error {
	if len(packages) > exemptionLimit {
		return fmt.Errorf("应用豁免最多 %d 个", exemptionLimit)
	}
	seen := map[string]bool{}
	for _, name := range packages {
		if !validPackage(name) || seen[name] {
			return fmt.Errorf("应用豁免包名无效或重复: %q", name)
		}
		seen[name] = true
	}
	return nil
}

func moduleJar(name string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	for _, dir := range []string{filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "..")} {
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("缺少模块工具 %s", name)
}

func runAndroidTool(jar, class string, args ...string) ([]byte, error) {
	path, err := moduleJar(jar)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/system/bin/app_process", append([]string{"/system/bin", class}, args...)...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "CLASSPATH=") {
			command.Env = append(command.Env, env)
		}
	}
	command.Env = append(command.Env, "CLASSPATH="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", class, err, output)
	}
	return output, nil
}

type InstalledApp struct {
	Package string `json:"package"`
	Label   string `json:"label"`
	UID     int    `json:"uid"`
	System  bool   `json:"system"`
}

func (s *Store) InstalledApps() ([]InstalledApp, error) {
	if s.RomRoot != "" && s.RomRoot != "/" {
		return nil, fmt.Errorf("应用枚举仅支持 Android 设备")
	}
	output, err := runAndroidTool("app-inventory.jar", "local.hyperfont.AppInventory")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	apps := []InstalledApp{}
	if err = json.Unmarshal([]byte(lines[len(lines)-1]), &apps); err != nil {
		return nil, err
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Package < apps[j].Package })
	return apps, nil
}

// UID resolution is one bounded PackageManager request at boot. An app ID is
// accepted only if ALL packages sharing it were selected. No process-name guess.
func compileExemptionIDs(packages []string, output string) ([]int, []string, error) {
	if err := validateExemptions(packages); err != nil {
		return nil, nil, err
	}
	selected := map[string]bool{}
	for _, p := range packages {
		selected[p] = true
	}
	groups := map[int][]string{}
	found := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "package:") || !strings.HasPrefix(fields[1], "uid:") {
			continue
		}
		name := strings.TrimPrefix(fields[0], "package:")
		uid, err := strconv.Atoi(strings.TrimPrefix(fields[1], "uid:"))
		if err != nil || uid < 0 || !validPackage(name) {
			continue
		}
		id := uid % 100000
		if id < 10000 || id >= 90000 {
			continue
		}
		groups[id] = append(groups[id], name)
		if selected[name] {
			found[name] = true
		}
	}
	ids := []int{}
	warnings := []string{}
	for _, p := range packages {
		if !found[p] {
			warnings = append(warnings, p+": 未安装，开机未启用豁免")
		}
	}
	for id, names := range groups {
		anySelected, allSelected := false, true
		for _, name := range names {
			anySelected = anySelected || selected[name]
			allSelected = allSelected && selected[name]
		}
		if anySelected && allSelected {
			ids = append(ids, id)
		}
		if anySelected && !allSelected {
			warnings = append(warnings, fmt.Sprintf("共享 UID %d: 需选择该 UID 的全部应用 (%s)", id, strings.Join(names, ", ")))
		}
	}
	sort.Ints(ids)
	sort.Strings(warnings)
	if len(packages) > 0 && len(groups) == 0 {
		return nil, nil, fmt.Errorf("无法解析已安装应用 UID，拒绝启用豁免")
	}
	return ids, warnings, nil
}

type ExemptionBoot struct {
	Packages []string `json:"packages"`
	AppIDs   []int    `json:"appIds"`
	Prepared bool     `json:"prepared"`
	Warnings []string `json:"warnings,omitempty"`
	MapBytes int64    `json:"mapBytes,omitempty"`
}

func (s *Store) ExemptionStatus() any {
	var boot BootRecord
	_ = readJSON(s.data("boot.json"), &boot)
	state := ExemptionBoot{Packages: []string{}, AppIDs: []int{}}
	if boot.BootID == bootID() && boot.State == "mounted" && boot.Exemptions != nil {
		state = *boot.Exemptions
	}
	return map[string]any{"boot": state, "requiresZygisk": true, "supportedSDKMin": 31, "supportedSDKMax": 35,
		"packageLimit": exemptionLimit, "isolatedProcessesSupported": false, "appRenderingVerified": false}
}

// Evidence files contain initialization milestones only. They do not claim a
// particular UI label uses the font, and they are never polled in background.
func (s *Store) ExemptionEvidence(packageName string) (any, error) {
	if !validPackage(packageName) {
		return nil, fmt.Errorf("应用包名无效")
	}
	apps, err := s.InstalledApps()
	if err != nil {
		return nil, err
	}
	id := -1
	for _, app := range apps {
		if app.Package == packageName {
			id = app.UID % 100000
		}
	}
	rows := []map[string]any{}
	entries, err := os.ReadDir(filepath.Join(s.Runtime, "exemptions", "results"))
	if os.IsNotExist(err) {
		return map[string]any{"processes": rows, "appRenderingVerified": false}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		parts := strings.Split(entry.Name(), "-")
		if len(parts) != 2 {
			continue
		}
		uid, e1 := strconv.Atoi(parts[0])
		pid, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil || uid%100000 != id {
			continue
		}
		if st, err := entry.Info(); err != nil || !st.Mode().IsRegular() || st.Size() > 4096 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Runtime, "exemptions", "results", entry.Name()))
		if err != nil {
			continue
		}
		// A stale PID alone cannot identify a currently running process.
		stat, err := os.ReadFile(filepath.Join("/proc", parts[1], "stat"))
		if err != nil {
			// On-demand cleanup keeps dead-process evidence from accumulating
			// during normal use; no daemon or startup directory scan is needed.
			if os.IsNotExist(err) {
				_ = os.Remove(filepath.Join(s.Runtime, "exemptions", "results", entry.Name()))
			}
			continue
		}
		fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
		lines := strings.Split(strings.TrimSpace(strings.TrimRight(string(b), "\x00")), "\n")
		if len(fields) < 20 || len(lines) == 0 || lines[0] != "start="+fields[19] {
			continue
		}
		rows = append(rows, map[string]any{"pid": pid, "uid": uid, "milestones": lines[1:]})
	}
	return map[string]any{"processes": rows, "appRenderingVerified": false}, nil
}
