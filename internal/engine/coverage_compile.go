package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func namedFamilies(set *node) []*node {
	result := []*node{}
	for _, n := range set.Children {
		if n.Name.Local == "family" {
			result = append(result, n)
		}
		if n.Name.Local == "family-list" {
			for _, f := range n.Children {
				if f.Name.Local == "family" {
					result = append(result, f)
				}
			}
		}
	}
	return result
}

// Remove inherited names before encoding to respect the family-list contract.
func inheritFamilyNames(set *node) func() {
	inherited := []*node{}
	for _, n := range set.Children {
		if n.Name.Local == "family-list" && n.attr("name") != "" {
			for _, f := range n.Children {
				if f.Name.Local == "family" && f.attr("name") == "" {
					f.set("name", n.attr("name"))
					inherited = append(inherited, f)
				}
			}
		}
	}
	return func() {
		for _, f := range inherited {
			f.del("name")
		}
	}
}
func coverageLanguages(rule CoverageRule) []string {
	if rule.Role == "emoji" {
		return []string{"und-Zsye"}
	}
	if rule.Role == "symbols" {
		return []string{"und-Zsym"}
	}
	if len(rule.Languages) > 0 {
		return rule.Languages
	}
	return []string{""}
}

func slotRole(key string) string {
	switch key {
	case "serif":
		return "serif"
	case "monospace":
		return "monospace"
	}
	return "sans"
}

func (c *compiler) maskPrimary(family *node, scope, dir string) error {
	for _, f := range family.Children {
		if f.Name.Local != "font" {
			continue
		}
		file := strings.TrimSpace(f.Text)
		index := 0
		if v := f.attr("index"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			index = n
		}
		var generated string
		if strings.HasPrefix(file, "HFC-") && c.references[file] {
			raw, err := os.ReadFile(filepath.Join(c.dir, "fonts", file))
			if err != nil {
				return err
			}
			out, err := RewriteFont(raw, index, "mask:"+scope)
			if err != nil {
				return err
			}
			generated = "HFC-" + digest(out) + ".ttf"
			if err = writeAtomic(filepath.Join(c.dir, "fonts", generated), out, 0644); err != nil {
				return err
			}
			c.references[generated] = true
		} else {
			if file == "" || filepath.Base(file) != file {
				return fmt.Errorf("非标准字体路径，需设备适配: %s", file)
			}
			var err error
			generated, err = c.fontFile(dir+"/"+file, index, "mask:"+scope, true)
			if err != nil {
				return err
			}
		}
		f.Text = generated
		f.del("index")
		f.del("postScriptName")
	}
	return nil
}

func coverageRuleMatches(rule CoverageRule, family *node) bool {
	name := family.attr("name")
	if len(rule.Families) > 0 {
		for _, target := range rule.Families {
			if target == name {
				return true
			}
		}
		return false
	}
	role := familyRole(name, family)
	if rule.Role == "emoji" || rule.Role == "symbols" {
		return role == "sans" || role == "serif" || role == "monospace" || role == "casual" || role == "cursive" || role == "decorative" || role == rule.Role
	}
	if role != rule.Role {
		return false
	}
	if len(rule.Languages) == 0 {
		return true
	}
	// Named primaries select the purpose; locale stays on their fallback chain.
	if name != "" {
		return true
	}
	for _, lang := range languageTags(family.attr("lang")) {
		for _, wanted := range rule.Languages {
			if lang == wanted || strings.HasPrefix(lang, wanted+"-") {
				return true
			}
		}
	}
	return false
}

func (c *compiler) appendChain(slot Slot, scope string, modern bool, name, lang string) ([]*node, error) {
	chain := append([]Slot{slot}, slot.Fallbacks...)
	result := []*node{}
	for _, member := range chain {
		fonts, err := c.makeFonts(member, scope, modern)
		if err != nil {
			return nil, err
		}
		family := el("family")
		if lang != "" {
			family.set("lang", lang)
		}
		if name != "" {
			for _, f := range fonts {
				f.set("fallbackFor", name)
			}
		}
		family.Children = fonts
		result = append(result, family)
	}
	return result, nil
}

func (c *compiler) coverageLayers(set *node, profile Profile, modern bool) (int, error) {
	config := profile.Coverage
	chains := false
	for _, slot := range profile.Slots {
		chains = chains || len(slot.Fallbacks) != 0
	}
	if config == nil && !chains {
		return 0, nil
	}
	if set.Name.Local == "fonts-modification" {
		return c.customizationCoverage(set, profile, modern)
	}
	changes := 0
	prepend := []*node{}
	namedExtras := []*node{}
	for _, family := range namedFamilies(set) {
		if family.Name.Local != "family" {
			continue
		}
		name := family.attr("name")
		if strings.HasPrefix(name, "00-hyperfont-") {
			continue
		}
		if config != nil && config.AutoFamilies && name != "" && namedSlot(name) == "" {
			role := familyRole(name, family)
			key := "latinSans"
			if role == "serif" {
				key = "serif"
			}
			if role == "monospace" {
				key = "monospace"
			}
			if role == "sans" || role == "serif" || role == "monospace" || role == "casual" || role == "cursive" || role == "decorative" {
				if profile.Slots[key].Mode != "system" {
					original := clone(family)
					original.del("name")
					original.del("customizationType")
					original.del("id")
					for _, f := range original.Children {
						if f.Name.Local == "font" {
							f.set("fallbackFor", name)
						}
					}
					namedExtras = append(namedExtras, original)
					fs, err := c.makeFonts(profile.Slots[key], key, modern)
					if err != nil {
						return 0, err
					}
					family.Children = fs
					changes++
					if profile.Slots["chinese"].Mode != "system" && !config.EngineCompatibility {
						for _, lang := range []string{"zh-Hans", "zh-Hant"} {
							chain, e := c.appendChain(profile.Slots["chinese"], "cjk", modern, name, lang)
							if e != nil {
								return 0, e
							}
							prepend = append(prepend, chain...)
						}
					}
				}
			}
		}
		if config == nil {
			continue
		}
		for _, rule := range config.Rules {
			if rule.Font.Mode == "system" || !coverageRuleMatches(rule, family) {
				continue
			}
			if name == "" {
				continue
			} // Locale-specific rules are installed below.
			// Mask the routed script on the primary and keep its original as a
			// later fallback. Locale rules never become a locale-free primary.
			original := clone(family)
			original.del("name")
			original.del("id")
			original.del("customizationType")
			for _, f := range original.Children {
				if f.Name.Local == "font" {
					f.set("fallbackFor", name)
				}
			}
			namedExtras = append(namedExtras, original)
			if err := c.maskPrimary(family, ruleScope(rule), "/system/fonts"); err != nil {
				return 0, err
			}
			family.Text = ""
			family.del("supportedAxes")
			changes++
		}
	}
	for _, key := range SlotNames {
		slot := profile.Slots[key]
		if slot.Mode == "system" {
			continue
		}
		if key != "chinese" && len(slot.Fallbacks) > 0 {
			for _, family := range namedFamilies(set) {
				targetKey := namedSlot(family.attr("name"))
				if targetKey == "" && config != nil && config.AutoFamilies && family.attr("name") != "" {
					switch familyRole(family.attr("name"), family) {
					case "sans", "casual", "cursive", "decorative":
						targetKey = "latinSans"
					case "serif":
						targetKey = "serif"
					case "monospace":
						targetKey = "monospace"
					}
				}
				if family.Name.Local == "family" && targetKey == key {
					for _, fallback := range slot.Fallbacks {
						fs, err := c.appendChain(fallback, coreScope(key), modern, family.attr("name"), "")
						if err != nil {
							return 0, err
						}
						prepend = append(prepend, fs...)
					}
				}
			}
			changes++
			if key == "latinSans" {
				fs, err := c.appendChain(slot, "latin", modern, "", "und-Latn")
				if err != nil {
					return 0, err
				}
				prepend = append(prepend, fs...)
			}
		}
	}
	if profile.Slots["chinese"].Mode != "system" && (chains || config != nil && config.EngineCompatibility) {
		slot := profile.Slots["chinese"]
		// Separate locales work with Android's parser and Skia versions which
		// split lang on whitespace. Generic fallback also covers no-locale calls;
		// Japanese/Korean locale-specific families retain their own priority.
		for _, lang := range []string{"zh-Hans", "zh-Hant", "zh", ""} {
			fs, err := c.appendChain(slot, "cjk", modern, "", lang)
			if err != nil {
				return 0, err
			}
			prepend = append(prepend, fs...)
		}
		for _, family := range namedFamilies(set) {
			if config == nil || !config.EngineCompatibility {
				break
			}
			name := family.attr("name")
			if name == "" || strings.HasPrefix(name, "00-hyperfont-") {
				continue
			}
			for _, lang := range []string{"zh-Hans", "zh-Hant", "zh", ""} {
				fs, err := c.appendChain(slot, "cjk", modern, name, lang)
				if err != nil {
					return 0, err
				}
				prepend = append(prepend, fs...)
			}
		}
		changes++
	}
	advanced := []*node{}
	if config != nil {
		for _, rule := range config.Rules {
			if rule.Font.Mode == "system" {
				continue
			}
			langs := rule.Languages
			if rule.Role == "emoji" {
				langs = []string{"und-Zsye"}
			} else if rule.Role == "symbols" {
				langs = []string{"und-Zsym"}
			}
			if len(langs) == 0 {
				langs = []string{""}
			}
			for _, lang := range langs {
				if len(rule.Families) == 0 && rule.Role != "serif" && rule.Role != "monospace" && rule.Role != "casual" && rule.Role != "cursive" && rule.Role != "decorative" {
					fs, err := c.appendChain(rule.Font, ruleScope(rule), modern, "", lang)
					if err != nil {
						return 0, err
					}
					advanced = append(advanced, fs...)
				}
				for _, family := range namedFamilies(set) {
					if family.Name.Local != "family" || !coverageRuleMatches(rule, family) || family.attr("name") == "" {
						continue
					}
					fs, err := c.appendChain(rule.Font, ruleScope(rule), modern, family.attr("name"), lang)
					if err != nil {
						return 0, err
					}
					advanced = append(advanced, fs...)
				}
			}
			changes++
		}
	}
	stock, err := c.productFallbacks(profile, modern)
	if err != nil {
		return 0, err
	}
	if len(stock) != 0 {
		changes++
	}
	prepend = append(advanced, prepend...)
	prepend = append(prepend, stock...)
	prepend = append(prepend, namedExtras...)
	if len(prepend) > 0 {
		at := len(set.Children)
		for i, n := range set.Children {
			if n.Name.Local == "family" && n.attr("name") == "" {
				at = i
				break
			}
		}
		children := append([]*node{}, set.Children[:at]...)
		children = append(children, prepend...)
		children = append(children, set.Children[at:]...)
		set.Children = children
	}
	return changes, nil
}

func coverageSelection(family *node, profile Profile) (Slot, string, bool) {
	config := profile.Coverage
	if config == nil {
		return Slot{}, "", false
	}
	for _, rule := range config.Rules {
		if rule.Font.Mode != "system" && coverageRuleMatches(rule, family) {
			return rule.Font, ruleScope(rule), true
		}
	}
	if !config.AutoFamilies {
		return Slot{}, "", false
	}
	role := familyRole(family.attr("name"), family)
	if role == "emoji" || role == "symbols" || role == "icons" || role == "unknown" {
		return Slot{}, "", false
	}
	key := "latinSans"
	if role == "serif" {
		key = "serif"
	}
	if role == "monospace" {
		key = "monospace"
	}
	return profile.Slots[key], coreScope(key), profile.Slots[key].Mode != "system"
}

func (c *compiler) customizationCoverage(set *node, profile Profile, modern bool) (int, error) {
	changes := 0
	for _, family := range namedFamilies(set) {
		if family.attr("name") == "" {
			continue
		}
		matched := false
		if profile.Coverage != nil {
			for _, rule := range profile.Coverage.Rules {
				if rule.Font.Mode == "system" || !coverageRuleMatches(rule, family) {
					continue
				}
				if err := c.maskPrimary(family, ruleScope(rule), "/product/fonts"); err != nil {
					return 0, err
				}
				changes++
				matched = true
			}
		}
		if matched {
			continue
		}
		slot, scope, ok := coverageSelection(family, profile)
		if !ok {
			continue
		}
		fonts, err := c.makeFonts(slot, scope, modern)
		if err != nil {
			return 0, err
		}
		family.Children = fonts
		family.Text = ""
		family.del("supportedAxes")
		changes++
	}
	return changes, nil
}

func (c *compiler) productFallbacks(profile Profile, modern bool) ([]*node, error) {
	priority, originals := []*node{}, []*node{}
	var base Baseline
	if err := readJSON(c.store.data("baseline.json"), &base); err != nil {
		return nil, err
	}
	for _, path := range ConfigPaths {
		if !strings.HasPrefix(path, "/product/") || base.Configs[path] == "" {
			continue
		}
		raw, err := os.ReadFile(c.store.data("baseline", strings.TrimPrefix(path, "/")))
		if err != nil {
			return nil, err
		}
		if digest(raw) != base.Configs[path] {
			return nil, fmt.Errorf("product 字体基线校验失败")
		}
		doc, err := parseXML(raw)
		if err != nil {
			return nil, err
		}
		for _, set := range doc.Children {
			undo := inheritFamilyNames(set)
			for _, n := range namedFamilies(set) {
				name := n.attr("name")
				if name == "" {
					continue
				}
				slot, scope, ok := coverageSelection(n, profile)
				if !ok {
					continue
				}
				matched := false
				for _, rule := range profile.Coverage.Rules {
					if rule.Font.Mode == "system" || !coverageRuleMatches(rule, n) {
						continue
					}
					matched = true
					for _, lang := range coverageLanguages(rule) {
						chain, e := c.appendChain(rule.Font, ruleScope(rule), modern, name, lang)
						if e != nil {
							return nil, e
						}
						priority = append(priority, chain...)
					}
				}
				if !matched {
					for _, fallback := range slot.Fallbacks {
						chain, e := c.appendChain(fallback, scope, modern, name, "")
						if e != nil {
							return nil, e
						}
						priority = append(priority, chain...)
					}
				}
				if profile.Slots["chinese"].Mode != "system" && (profile.Coverage.EngineCompatibility || profile.Coverage.AutoFamilies) {
					for _, lang := range []string{"zh-Hans", "zh-Hant", "zh", ""} {
						chain, e := c.appendChain(profile.Slots["chinese"], "cjk", modern, name, lang)
						if e != nil {
							return nil, e
						}
						priority = append(priority, chain...)
					}
				}
				original := clone(n)
				original.del("name")
				original.del("id")
				original.del("customizationType")
				for _, f := range original.Children {
					if f.Name.Local != "font" {
						continue
					}
					file := strings.TrimSpace(f.Text)
					if file == "" || strings.ContainsAny(file, "/\\") {
						return nil, fmt.Errorf("%s: 非标准 product 字体路径", name)
					}
					index := 0
					if v := f.attr("index"); v != "" {
						index, err = strconv.Atoi(v)
						if err != nil {
							return nil, err
						}
					}
					f.Text, err = c.fontFile("/product/fonts/"+file, index, "all", true)
					if err != nil {
						return nil, err
					}
					f.set("fallbackFor", name)
					f.del("index")
					f.del("postScriptName")
				}
				originals = append(originals, original)
			}
			undo()
		}
	}
	return append(priority, originals...), nil
}
