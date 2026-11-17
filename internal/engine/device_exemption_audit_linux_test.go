//go:build linux

package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDeviceExemptionInitialization(t *testing.T) {
	if os.Getenv("HFC_EXEMPTION_AUDIT") != "1" {
		t.Skip("opt-in isolated Android diagnostic")
	}
	self, _ := os.Readlink("/proc/self/ns/mnt")
	init, _ := os.Readlink("/proc/1/ns/mnt")
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil || os.Getuid() != 0 || self == init || strings.Contains(string(info), " shared:") || strings.Contains(string(info), " master:") {
		t.Fatal("requires private root mount namespace")
	}
	s := Store{Root: "/data/adb/hyperfont-control", RomRoot: "/", Runtime: "/dev/hyperfont-control"}
	g, err := s.generation("active")
	if err != nil || g == nil {
		t.Fatal("no active generation", err)
	}
	var base Baseline
	if err = readJSON(s.data("baseline.json"), &base); err != nil {
		t.Fatal(err)
	}
	originalRuntime := s.Runtime
	// Shadow only in this test's mount namespace. Original mirrors stay visible
	// through the open source namespace until they are rebound below.
	temp, err := os.MkdirTemp("/dev", "hfc-exemption-audit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(temp)
	for _, dir := range []string{"/system/fonts", "/product/fonts"} {
		source := filepath.Join(originalRuntime, "original", strings.TrimPrefix(dir, "/"))
		target := filepath.Join(temp, strings.TrimPrefix(dir, "/"))
		if err = os.MkdirAll(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err = bind(source, target, true); err != nil {
			t.Fatal(err)
		}
		defer syscall.Unmount(target, syscall.MNT_DETACH)
	}
	// Capture the currently replaced map BEFORE any original views are exposed.
	mounts, err := s.configMountTargets(*g)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/system/fonts", "/product/fonts"} {
		if err = bind(filepath.Join(temp, strings.TrimPrefix(dir, "/")), dir, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range mounts {
		path := filepath.Join(temp, "xml", strings.TrimPrefix(target.Path, "/"))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(s.data("baseline", strings.TrimPrefix(target.Path, "/")))
		if err != nil {
			t.Fatal(err)
		}
		if digest(raw) != base.Configs[target.Path] {
			t.Fatal("invalid stock baseline")
		}
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", path).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		if err = bind(path, target.Path, false); err != nil {
			t.Fatal(err)
		}
	}
	jar := os.Getenv("HFC_AUDIT_JAR")
	lib := os.Getenv("HFC_AUDIT_LIB")
	stock := filepath.Join(temp, "stock-font-map")
	command := exec.Command("/system/bin/app_process", "/system/bin", "local.hyperfont.StockFontMap", stock)
	command.Env = append(os.Environ(), "CLASSPATH="+jar)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", stock).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	// Re-expose global HFC views locally so the probe receives a replacement
	// map and exercises restoration against those mounts.
	for _, dir := range []string{"/system/fonts", "/product/fonts"} {
		if err = bind(filepath.Join(originalRuntime, "merged", strings.TrimPrefix(dir, "/")), dir, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range mounts {
		if err = bind(s.data("generations", g.ID, target.File), target.Path, false); err != nil {
			t.Fatal(err)
		}
	}
	runtime := filepath.Join(temp, "runtime")
	if err = os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err = bind(runtime, originalRuntime, false); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(originalRuntime, syscall.MNT_DETACH)
	for _, dir := range []string{"system/fonts", "product/fonts"} {
		path := filepath.Join(originalRuntime, "original", dir)
		if err = os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err = bind(filepath.Join(temp, dir), path, true); err != nil {
			t.Fatal(err)
		}
		defer syscall.Unmount(path, syscall.MNT_DETACH)
	}
	var manifest strings.Builder
	manifest.WriteString("hyperfont-exempt-v1\n")
	fmt.Fprintf(&manifest, "sdk=%d\nuid=10000\n", base.SDK)
	for _, target := range mounts {
		path := filepath.Join(originalRuntime, "exemptions", "original", strings.TrimPrefix(target.Path, "/"))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(s.data("baseline", strings.TrimPrefix(target.Path, "/")))
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", path).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		fmt.Fprintf(&manifest, "xml=%s\n", target.Path)
	}
	if err = os.WriteFile(filepath.Join(originalRuntime, "exemptions", "manifest"), []byte(manifest.String()), 0400); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("/system/bin/app_process", "/system/bin", "local.hyperfont.ExemptionProbe", lib, stock)
	command.Env = append(os.Environ(), "CLASSPATH="+jar)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result struct {
		DescriptorSubstituted bool
		FilesRestored         bool
		IncomingBytes         int
		StockBytes            int
		Glyphs                []struct {
			File  string
			Glyph int
		}
	}
	if err = json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil {
		t.Fatal(err, string(out))
	}
	if !result.DescriptorSubstituted || !result.FilesRestored || len(result.Glyphs) < 20 {
		t.Fatal("incomplete probe", string(out))
	}
	for _, glyph := range result.Glyphs {
		if strings.Contains(glyph.File, "HFC-") || glyph.Glyph == 0 {
			t.Fatal("replacement or missing glyph", string(out))
		}
	}
	t.Log("EXEMPTION_INITIALIZATION_PASS", string(out))
}
