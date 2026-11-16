package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CoverageReport struct {
	Inventory            FontInventory       `json:"inventory"`
	Families             []CoverageFamily    `json:"families"`
	Sample               []CoverageCharacter `json:"sample"`
	Warnings             []string            `json:"warnings"`
	Verification         string              `json:"verification"`
	EngineCompatibility  bool                `json:"engineCompatibility"`
	AppRenderingVerified bool                `json:"appRenderingVerified"`
}
type CoverageFamily struct {
	Name   string `json:"name"`
	Config string `json:"config"`
	Role   string `json:"role"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}
type CoverageCharacter struct {
	Character string `json:"character"`
	Codepoint string `json:"codepoint"`
	Source    string `json:"source"`
	Scope     string `json:"scope,omitempty"`
}

// Reports use an isolated temporary generation; they never stage, mount, resnapshot
// a baseline or change the saved draft. Sample results predict coverage only.
func (s *Store) CoverageReport(profile Profile, sample, language, role string) (CoverageReport, error) {
	result := CoverageReport{Families: []CoverageFamily{}, Sample: []CoverageCharacter{}, Verification: "static-coverage-prediction", Warnings: []string{}}
	warnings, err := s.Validate(profile)
	if err != nil {
		return result, err
	}
	result.Warnings = warnings
	if len([]rune(sample)) > 512 {
		return result, fmt.Errorf("覆盖检查最多 512 个字符")
	}
	if language != "" && !validLanguage(language) {
		return result, fmt.Errorf("语言标签无效")
	}
	if role == "" {
		role = "sans"
	}
	family := role
	switch role {
	case "sans-serif":
		role = "sans"
	case "sans", "serif", "monospace", "casual", "cursive", "decorative", "emoji", "symbols", "icons":
	default:
		if !safeFamily(role) {
			return result, fmt.Errorf("字体族无效")
		}
		role = "unknown"
	}
	if family == "sans" {
		family = "sans-serif"
	}
	inv, err := s.FontInventory()
	if err != nil {
		return result, err
	}
	result.Inventory = inv
	result.EngineCompatibility = profile.Coverage != nil && profile.Coverage.EngineCompatibility
	var base Baseline
	baseErr := readJSON(s.data("baseline.json"), &base)
	if baseErr != nil && !os.IsNotExist(baseErr) {
		return result, baseErr
	}
	if baseErr == nil && len(base.Configs) > 0 && !profile.IsSystem() {
		dir, err := os.MkdirTemp("", "hyperfont-coverage-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(dir)
		c := compiler{s, dir, map[string]string{}, map[string]string{}, map[string]bool{}}
		for _, path := range ConfigPaths {
			if base.Configs[path] == "" {
				continue
			}
			raw, err := os.ReadFile(s.data("baseline", strings.TrimPrefix(path, "/")))
			if err != nil {
				return result, err
			}
			if digest(raw) != base.Configs[path] {
				return result, fmt.Errorf("原生字体基线校验失败")
			}
			out, _, err := c.compileXML(path, raw, profile)
			if err != nil {
				return result, fmt.Errorf("%s: %w", path, err)
			}
			families, _, err := inventoryDocument(out, path)
			if err != nil {
				return result, err
			}
			plannedFor := map[string]bool{}
			for _, f := range families {
				if f.Generated {
					for _, name := range f.FallbackFor {
						plannedFor[name] = true
					}
				}
			}
			for _, family := range families {
				if family.Name == "" || strings.HasPrefix(family.Name, "00-hyperfont-") {
					continue
				}
				state, reason := "system", "沿用原生字体或未配置此字体族"
				if family.Generated || plannedFor[family.Name] {
					state = "planned"
					reason = "已生成替换或字符过滤；缺字由回退链补字"
				}
				result.Families = append(result.Families, CoverageFamily{Name: family.Name, Config: path, Role: family.Role, State: state, Reason: reason})
			}
		}
	} else {
		if !profile.IsSystem() {
			result.Warnings = append(result.Warnings, "原生基线尚未建立；本次只预测所选字体的码点，不代表配置已编译或应用。")
		}
		for _, f := range inv.Families {
			if f.Name != "" {
				result.Families = append(result.Families, CoverageFamily{Name: f.Name, Config: f.Config, Role: f.Role, State: "system", Reason: "原生基线尚未建立或当前方案沿用系统"})
			}
		}
	}
	type candidate struct {
		source, scope string
		ref           FaceRef
	}
	candidates := []candidate{}
	if profile.Coverage != nil {
		for _, rule := range profile.Coverage.Rules {
			matchesFamily := len(rule.Families) == 0 && (rule.Role == role || rule.Role == "emoji" || rule.Role == "symbols")
			for _, target := range rule.Families {
				matchesFamily = matchesFamily || target == family
			}
			if !matchesFamily || rule.Font.Mode == "system" {
				continue
			}
			matches := len(rule.Languages) == 0
			for _, lang := range rule.Languages {
				matches = matches || lang == language || strings.HasPrefix(language, lang+"-")
			}
			if matches {
				for _, ref := range slotReferences(rule.Font) {
					candidates = append(candidates, candidate{rule.Label + " " + rule.ID, ruleScope(rule), ref})
				}
			}
		}
	}
	key := "latinSans"
	if role == "serif" {
		key = "serif"
	}
	if role == "monospace" {
		key = "monospace"
	}
	for _, k := range []string{key, "chinese"} {
		if profile.Slots[k].Mode != "system" {
			for _, ref := range slotReferences(profile.Slots[k]) {
				candidates = append(candidates, candidate{slotTitles[k], coreScope(k), ref})
			}
		}
	}
	maps := map[string]map[uint32]uint16{}
	for _, candidate := range candidates {
		key := fmt.Sprintf("%s:%d", candidate.ref.AssetID, candidate.ref.FaceIndex)
		if maps[key] != nil {
			continue
		}
		b, err := os.ReadFile(s.data("library", candidate.ref.AssetID+".font"))
		if err != nil {
			return result, err
		}
		cm, err := readFaceCharmap(b, candidate.ref.FaceIndex)
		if digest(b) != candidate.ref.AssetID {
			return result, fmt.Errorf("字体文件校验失败")
		}
		if err != nil {
			return result, err
		}
		maps[key] = cm
	}
	seen := map[rune]bool{}
	for _, ch := range sample {
		if seen[ch] {
			continue
		}
		seen[ch] = true
		row := CoverageCharacter{Character: string(ch), Codepoint: fmt.Sprintf("U+%04X", ch), Source: "system-fallback-unverified"}
		for _, candidate := range candidates {
			if scopeKeeps(uint32(ch), candidate.scope) && maps[fmt.Sprintf("%s:%d", candidate.ref.AssetID, candidate.ref.FaceIndex)][uint32(ch)] != 0 {
				row.Source = candidate.source
				row.Scope = candidate.scope
				break
			}
		}
		result.Sample = append(result.Sample, row)
	}
	sort.SliceStable(result.Families, func(i, j int) bool {
		if result.Families[i].Config != result.Families[j].Config {
			return result.Families[i].Config < result.Families[j].Config
		}
		return result.Families[i].Name < result.Families[j].Name
	})
	return result, nil
}

func readFaceCharmap(b []byte, index int) (cm map[uint32]uint16, err error) {
	defer fontRecover(&err)
	offs := offsets(fontData(b))
	if index < 0 || index >= len(offs) {
		return nil, fmt.Errorf("字体面索引无效")
	}
	return charmap(tables(fontData(b), offs[index])), nil
}

func (s *Store) AppFontEvidence(packageName string) (any, error) {
	if len(packageName) > 160 || !strings.Contains(packageName, ".") {
		return nil, fmt.Errorf("应用包名无效")
	}
	for _, c := range packageName {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_') {
			return nil, fmt.Errorf("应用包名无效")
		}
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, entry := range entries {
		pid := entry.Name()
		numeric := pid != ""
		for _, c := range pid {
			numeric = numeric && c >= '0' && c <= '9'
		}
		if !numeric {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		if err != nil {
			continue
		}
		name := strings.Split(string(cmd), "\x00")[0]
		if name != packageName && !strings.HasPrefix(name, packageName+":") {
			continue
		}
		maps, err := os.ReadFile(filepath.Join("/proc", pid, "maps"))
		if err != nil {
			rows = append(rows, map[string]any{"pid": pid, "error": err.Error()})
			continue
		}
		fonts := map[string]bool{}
		engines := map[string]bool{}
		for _, line := range strings.Split(string(maps), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 6 {
				continue
			}
			path := fields[5]
			lower := strings.ToLower(path)
			if strings.Contains(lower, "/fonts/") || strings.Contains(lower, "/font-family/") || strings.HasSuffix(lower, ".ttf") || strings.HasSuffix(lower, ".otf") || strings.HasSuffix(lower, ".ttc") || strings.Contains(lower, ".woff") {
				fonts[path] = true
			}
			for _, engine := range []string{"libunicorn.so", "libflutter.so", "libwebviewuc.so", "libweexcore.so"} {
				if strings.HasSuffix(lower, "/"+engine) {
					engines[engine] = true
				}
			}
		}
		fontList := []string{}
		for path := range fonts {
			fontList = append(fontList, path)
		}
		sort.Strings(fontList)
		engineList := []string{}
		for engine := range engines {
			engineList = append(engineList, engine)
		}
		sort.Strings(engineList)
		rows = append(rows, map[string]any{"pid": pid, "process": name, "mappedFonts": fontList, "engines": engineList})
	}
	return map[string]any{"package": packageName, "processes": rows, "verification": "mapped-files-only", "glyphSelectionVerified": false}, nil
}
