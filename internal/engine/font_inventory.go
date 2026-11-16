package engine

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type FontInventory struct {
	Version  string            `json:"version"`
	Scripts  []string          `json:"scripts"`
	Families []InventoryFamily `json:"families"`
	Aliases  []InventoryAlias  `json:"aliases"`
	Errors   []string          `json:"errors"`
}
type InventoryFamily struct {
	ID          string   `json:"id"`
	Config      string   `json:"config"`
	Name        string   `json:"name,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	Variant     string   `json:"variant,omitempty"`
	Role        string   `json:"role"`
	Files       []string `json:"files"`
	FallbackFor []string `json:"fallbackFor,omitempty"`
	Generated   bool     `json:"generated"`
	State       string   `json:"state"`
}
type InventoryAlias struct {
	Config   string `json:"config"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Resolved string `json:"resolved,omitempty"`
	Weight   string `json:"weight,omitempty"`
	Error    string `json:"error,omitempty"`
}

func languageTags(value string) []string {
	return strings.FieldsFunc(value, func(c rune) bool { return c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' })
}

func familyRole(name string, family *node) string {
	if family.role != "" {
		return family.role
	}
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "icon") {
		return "icons"
	}
	if strings.Contains(lowerName, "emoji") {
		return "emoji"
	}
	switch {
	case name == "monospace" || strings.Contains(name, "monospace"):
		return "monospace"
	case name == "serif" || strings.HasPrefix(name, "serif-"):
		return "serif"
	case name == "casual":
		return "casual"
	case name == "cursive":
		return "cursive"
	case name == "fantasy":
		return "decorative"
	case namedSlot(name) == "latinSans":
		return "sans"
	case strings.Contains(lowerName, "sans") || strings.HasPrefix(lowerName, "x-sst") || lowerName == "sst":
		return "sans"
	}
	for _, f := range family.Children {
		if f.Name.Local != "font" {
			continue
		}
		file := strings.ToLower(strings.TrimSpace(f.Text))
		if strings.Contains(file, "emoji") || strings.Contains(family.attr("lang"), "Zsye") {
			return "emoji"
		}
		if strings.Contains(file, "symbol") || strings.Contains(family.attr("lang"), "Zsym") {
			return "symbols"
		}
		if strings.Contains(file, "icon") {
			return "icons"
		}
		if strings.Contains(file, "mono") {
			return "monospace"
		}
		if strings.Contains(file, "serif") && !strings.Contains(file, "sans") {
			return "serif"
		}
		if strings.Contains(file, "sans") || strings.Contains(file, "gothic") || strings.HasPrefix(file, "sst") {
			return "sans"
		}
	}
	if name == "" {
		return "sans"
	}
	return "unknown"
}

func inventoryDocument(raw []byte, path string) ([]InventoryFamily, []InventoryAlias, error) {
	doc, err := parseXML(raw)
	if err != nil {
		return nil, nil, err
	}
	families := []InventoryFamily{}
	aliases := []InventoryAlias{}
	var walk func(*node, string)
	walk = func(n *node, inherited string) {
		if n.Name.Local == "family-list" {
			inherited = n.attr("name")
		}
		if n.Name.Local == "family" {
			name := n.attr("name")
			if name == "" {
				name = inherited
			}
			row := InventoryFamily{ID: fmt.Sprintf("%s#%d", path, len(families)), Config: path, Name: name, Languages: languageTags(n.attr("lang")), Variant: n.attr("variant"), Role: familyRole(name, n), Files: []string{}, FallbackFor: []string{}, State: "system"}
			for _, f := range n.Children {
				if f.Name.Local == "font" {
					file := strings.TrimSpace(f.Text)
					row.Files = append(row.Files, file)
					if strings.HasPrefix(file, "HFC-") {
						row.Generated = true
						row.State = "generated"
					}
					if v := f.attr("fallbackFor"); v != "" {
						row.FallbackFor = append(row.FallbackFor, v)
					}
				}
			}
			families = append(families, row)
		}
		if n.Name.Local == "alias" {
			aliases = append(aliases, InventoryAlias{Config: path, Name: n.attr("name"), Target: n.attr("to"), Weight: n.attr("weight")})
		}
		for _, child := range n.Children {
			walk(child, inherited)
		}
	}
	walk(doc, "")
	return families, aliases, nil
}

func (s *Store) FontInventory() (FontInventory, error) {
	result := FontInventory{Version: Version, Scripts: scriptList(), Families: []InventoryFamily{}, Aliases: []InventoryAlias{}, Errors: []string{}}
	for _, path := range ConfigPaths {
		raw, err := os.ReadFile(s.rom(path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			result.Errors = append(result.Errors, path+": "+err.Error())
			continue
		}
		f, a, err := inventoryDocument(raw, path)
		if err != nil {
			result.Errors = append(result.Errors, path+": "+err.Error())
			continue
		}
		result.Families = append(result.Families, f...)
		result.Aliases = append(result.Aliases, a...)
	}
	names := map[string]bool{}
	targets := map[string]string{}
	for _, f := range result.Families {
		if f.Name != "" {
			names[f.Name] = true
		}
	}
	for _, a := range result.Aliases {
		targets[a.Name] = a.Target
	}
	for i := range result.Aliases {
		a := &result.Aliases[i]
		target := a.Target
		seen := map[string]bool{a.Name: true}
		for steps := 0; steps < 64; steps++ {
			if names[target] {
				a.Resolved = target
				break
			}
			if seen[target] {
				a.Error = "字体别名循环"
				break
			}
			seen[target] = true
			next, ok := targets[target]
			if !ok {
				a.Error = "别名目标未识别"
				break
			}
			target = next
		}
		if a.Resolved == "" && a.Error == "" {
			a.Error = "字体别名链过长"
		}
	}
	sort.Strings(result.Errors)
	return result, nil
}
