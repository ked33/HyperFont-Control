//go:build linux

package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Store) prepareExemptions(g Generation, mounts []Target) (*ExemptionBoot, error) {
	state := &ExemptionBoot{Packages: append([]string{}, g.Profile.ExemptPackages...), AppIDs: []int{}}
	if len(state.Packages) == 0 {
		return state, nil
	}
	sdk, _ := strconv.Atoi(property("ro.build.version.sdk"))
	if sdk < 31 || sdk > 35 {
		return nil, fmt.Errorf("应用豁免目前支持 Android 12–15，当前 SDK %d", sdk)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Read the boot package database rather than start PackageManager before
	// system_server exists. Format is maintained by PackageManager at install.
	raw, err := os.ReadFile("/data/system/packages.list")
	if err != nil {
		return nil, fmt.Errorf("读取开机应用 UID: %w", err)
	}
	var packages strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			fmt.Fprintf(&packages, "package:%s uid:%s\n", fields[0], fields[1])
		}
	}
	state.AppIDs, state.Warnings, err = compileExemptionIDs(state.Packages, packages.String())
	if err != nil {
		return nil, err
	}
	if len(state.AppIDs) == 0 {
		return state, nil
	}
	dir := filepath.Join(s.Runtime, "exemptions")
	if err = os.MkdirAll(filepath.Join(dir, "results"), 0700); err != nil {
		return nil, err
	}
	var manifest strings.Builder
	manifest.WriteString("hyperfont-exempt-v1\n")
	fmt.Fprintf(&manifest, "sdk=%d\n", sdk)
	for _, id := range state.AppIDs {
		fmt.Fprintf(&manifest, "uid=%d\n", id)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !validPackage(fields[0]) {
			continue
		}
		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		for _, allowed := range state.AppIDs {
			if uid%100000 == allowed {
				fmt.Fprintf(&manifest, "pkg=%d:%s\n", allowed, fields[0])
				break
			}
		}
	}
	var base Baseline
	if err = readJSON(s.data("baseline.json"), &base); err != nil {
		return nil, err
	}
	for _, t := range mounts {
		raw, err := os.ReadFile(s.data("baseline", strings.TrimPrefix(t.Path, "/")))
		if err != nil || digest(raw) != base.Configs[t.Path] {
			return nil, fmt.Errorf("原生字体表校验失败: %s", t.Path)
		}
		path := filepath.Join(dir, "original", strings.TrimPrefix(t.Path, "/"))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(path, raw, 0444); err != nil {
			return nil, err
		}
		if out, err := exec.CommandContext(ctx, "/system/bin/chcon", "u:object_r:system_file:s0", path).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("原生字体表标签: %s", out)
		}
		fmt.Fprintf(&manifest, "xml=%s\n", t.Path)
	}
	// Existing views are still stock at this point. The serialized map is
	// generated once; all exempt processes map this SAME immutable file.
	mapPath := filepath.Join(dir, "stock-font-map")
	if _, err = runAndroidTool("stock-font-map.jar", "local.hyperfont.StockFontMap", mapPath); err != nil {
		return nil, err
	}
	st, err := os.Stat(mapPath)
	if err != nil || st.Size() < 8 || st.Size() > 32<<20 {
		return nil, fmt.Errorf("原生字体映射大小无效")
	}
	state.MapBytes = st.Size()
	if err = os.Chmod(mapPath, 0444); err != nil {
		return nil, err
	}
	if out, err := exec.CommandContext(ctx, "/system/bin/chcon", "u:object_r:system_file:s0", mapPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("原生字体映射标签: %s", out)
	}
	// Publish last; an incomplete preparation never activates a process hook.
	// Zygote may read these before specialization; root-only permissions remain.
	if out, err := exec.Command("/system/bin/chcon", "-R", "u:object_r:system_file:s0", dir).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("豁免运行目录标签: %s", out)
	}
	// Zygote's private temporary type allows creating startup evidence before
	// sandboxing without granting it writes to system_file.
	if out, err := exec.Command("/system/bin/chcon", "u:object_r:zygote_tmpfs:s0", filepath.Join(dir, "results")).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("豁免诊断目录标签: %s", out)
	}
	if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", s.Runtime).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("豁免运行入口标签: %s", out)
	}
	if err = writeAtomic(filepath.Join(dir, "manifest"), []byte(manifest.String()), 0400); err != nil {
		return nil, err
	}
	if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", filepath.Join(dir, "manifest")).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("豁免名单标签: %s", out)
	}
	state.Prepared = true
	return state, nil
}
