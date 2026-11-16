package engine

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func osReadFont(s *Store, id string) ([]byte, error) {
	return os.ReadFile(s.data("library", id+".font"))
}

// Coverage remains optional so existing four-slot profiles retain their meaning.
type CoverageConfig struct {
	AutoFamilies        bool           `json:"autoFamilies,omitempty"`
	EngineCompatibility bool           `json:"engineCompatibility,omitempty"`
	Rules               []CoverageRule `json:"rules,omitempty"`
}

type CoverageRule struct {
	ID        string   `json:"id"`
	Label     string   `json:"label,omitempty"`
	Role      string   `json:"role"`
	Scripts   []string `json:"scripts,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Families  []string `json:"families,omitempty"`
	Font      Slot     `json:"font"`
}

func coreScope(key string) string {
	if key == "chinese" {
		return "cjk"
	}
	return "latin"
}

func ruleScope(rule CoverageRule) string {
	switch rule.Role {
	case "emoji":
		return "emoji"
	case "symbols":
		return "symbols"
	case "icons":
		return "private-use"
	}
	return "script:" + strings.Join(rule.Scripts, ",")
}

func safeFamily(name string) bool {
	if name == "" || len(name) > 160 || strings.HasPrefix(name, "00-hyperfont-") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ' ' || c == '.') {
			return false
		}
	}
	return true
}

func validLanguage(tag string) bool {
	if len(tag) < 2 || len(tag) > 64 {
		return false
	}
	for _, part := range strings.Split(tag, "-") {
		if part == "" || len(part) > 8 {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				return false
			}
		}
	}
	return true
}

func (s *Store) validateCoverage(config CoverageConfig) ([]string, error) {
	if len(config.Rules) > 32 {
		return nil, fmt.Errorf("高级覆盖最多 32 条规则")
	}
	warnings := []string{}
	ids := map[string]bool{}
	for _, rule := range config.Rules {
		if !safeFamily(rule.ID) || ids[rule.ID] || len([]rune(rule.Label)) > 80 {
			return nil, fmt.Errorf("高级覆盖规则名称或 ID 无效")
		}
		ids[rule.ID] = true
		switch rule.Role {
		case "sans", "serif", "monospace", "casual", "cursive", "decorative", "emoji", "symbols", "icons":
		default:
			return nil, fmt.Errorf("%s: 字体用途无效", rule.ID)
		}
		special := rule.Role == "emoji" || rule.Role == "symbols" || rule.Role == "icons"
		if len(rule.Scripts) > 12 || !special && len(rule.Scripts) == 0 || special && len(rule.Scripts) != 0 {
			return nil, fmt.Errorf("%s: 请选择文字系统，特殊字体不接受文字系统参数", rule.ID)
		}
		seen := map[string]bool{}
		for _, script := range rule.Scripts {
			if !knownScript(script) || script == "Zyyy" || script == "Zinh" || script == "Zzzz" || seen[script] {
				return nil, fmt.Errorf("%s: 文字系统无效或重复", rule.ID)
			}
			seen[script] = true
		}
		if len(rule.Languages) > 12 || len(rule.Families) > 32 {
			return nil, fmt.Errorf("%s: 语言或字体族过多", rule.ID)
		}
		for _, lang := range rule.Languages {
			if !validLanguage(lang) {
				return nil, fmt.Errorf("%s: 语言标签无效", rule.ID)
			}
		}
		for _, name := range rule.Families {
			if !safeFamily(name) {
				return nil, fmt.Errorf("%s: 字体族名称无效", rule.ID)
			}
		}
		if seen["Hani"] && len(rule.Languages) == 0 {
			return nil, fmt.Errorf("%s: 汉字覆盖须指定 zh-Hans、zh-Hant、ja 或 ko 等语言", rule.ID)
		}
		if rule.Role == "icons" && len(rule.Families) == 0 {
			return nil, fmt.Errorf("%s: 图标必须指定具体字体族，不能全局替换私用区", rule.ID)
		}
		w, err := s.validateSlot(rule.Font, rule.ID, ruleScope(rule), true)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, w...)
		if rule.Role == "monospace" && rule.Font.Mode != "system" {
			for _, ref := range slotReferences(rule.Font) {
				_, face, err := s.lookup(ref.AssetID, ref.FaceIndex)
				if err != nil {
					return nil, err
				}
				if !face.Monospace {
					warnings = append(warnings, rule.ID+": 字体未通过等宽检测，中英文混排列宽仍需实测。")
				}
			}
		}
		if special && rule.Font.Mode != "system" {
			warnings = append(warnings, rule.ID+": 特殊字体需验收组合序列、彩色格式和图标编码，不保证仅有码点即正确显示。")
		}
	}
	return warnings, nil
}

func (s *Store) checkFaceScope(face Face, slot Slot, scope string) error {
	if scope == "emoji" && len(face.ColorFormats) == 0 {
		return fmt.Errorf("表情替换需要彩色字体")
	}
	if scope == "emoji" && face.Emoji || scope == "symbols" && face.Symbols || scope == "private-use" && face.PrivateUse {
		return nil
	}
	for _, script := range strings.Split(strings.TrimPrefix(scope, "script:"), ",") {
		for _, present := range face.Scripts {
			if present == script {
				return nil
			}
		}
	}
	return fmt.Errorf("字体没有所选文字范围的字符")
}

func slotReferences(slot Slot) []FaceRef {
	refs := append([]FaceRef{}, slot.Faces...)
	if slot.AssetID != "" {
		refs = append(refs, FaceRef{AssetID: slot.AssetID, FaceIndex: slot.FaceIndex})
	}
	if slot.ItalicAssetID != "" {
		refs = append(refs, FaceRef{AssetID: slot.ItalicAssetID, FaceIndex: slot.ItalicFaceIndex})
	}
	for _, fallback := range slot.Fallbacks {
		refs = append(refs, slotReferences(fallback)...)
	}
	return refs
}

func profileSlots(profile Profile) []Slot {
	result := make([]Slot, 0, len(profile.Slots))
	for _, key := range SlotNames {
		result = append(result, profile.Slots[key])
	}
	if profile.Coverage != nil {
		for _, rule := range profile.Coverage.Rules {
			result = append(result, rule.Font)
		}
	}
	return result
}

func scriptList() []string {
	seen := map[string]bool{}
	for _, r := range unicodeScripts {
		if r.scripts != "Zyyy" && r.scripts != "Zinh" && r.scripts != "Zzzz" {
			seen[r.scripts] = true
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func hasWeightAxis(face Face) bool {
	for _, axis := range face.Axes {
		if axis.Tag == "wght" {
			return true
		}
	}
	return false
}
