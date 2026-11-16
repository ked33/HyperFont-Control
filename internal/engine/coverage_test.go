package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageLocaleRulesKeepPrimaryScripts(t *testing.T) {
	s, a, b := fixtureStore(t)
	p := DefaultProfile()
	slot := variableProfile(a).Slots["chinese"]
	slot.Fallbacks = []Slot{{Mode: "static", MissingWeightPolicy: "nearest", Faces: []FaceRef{{AssetID: b.ID, Weight: 400, Style: "normal"}}}}
	p.Coverage = &CoverageConfig{Rules: []CoverageRule{{ID: "han-serif", Role: "serif", Scripts: []string{"Hani"}, Languages: []string{"zh-Hans"}, Font: slot}}}
	if _, err := s.Stage(DefaultProfile()); err != nil {
		t.Fatal(err)
	}
	c := compiler{s, t.TempDir(), map[string]string{}, map[string]string{}, map[string]bool{}}
	raw := []byte(`<familyset><family name="serif"><font>Stock.ttf</font></family><family lang="ja"><font>Stock.ttf</font></family><family lang="und-Zsye"><font>Emoji.ttf</font></family></familyset>`)
	out, _, err := c.compileXML("/system/etc/fonts.xml", raw, p)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseXML(out)
	if err != nil {
		t.Fatal(err)
	}
	set := doc.Children[0]
	first := set.Children[0]
	font := first.Children[0]
	data, err := os.ReadFile(filepath.Join(c.dir, "fonts", font.Text))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := readFaceCharmap(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cm['A'] == 0 || cm[0x4e2d] != 0 {
		t.Fatal("locale rule replaced unrelated primary characters")
	}
	routed := []*node{}
	for _, f := range set.Children {
		if f.attr("lang") == "zh-Hans" && len(f.Children) > 0 && f.Children[0].attr("fallbackFor") == "serif" {
			routed = append(routed, f)
		}
	}
	if len(routed) != 2 {
		t.Fatalf("want primary + ordered supplement, got %d", len(routed))
	}
	if !bytes.Contains(out, []byte(`lang="ja"`)) || !bytes.Contains(out, []byte("Emoji.ttf")) {
		t.Fatal("native fallback lost")
	}
	if strings.Contains(string(out), `fallbackFor="serif" lang=""`) {
		t.Fatal("locale-free route")
	}
	if len(profileAssets(p)) != 2 {
		t.Fatal("supplement reference not retained")
	}
}

func TestCoverageInventoryAndFamilyListContract(t *testing.T) {
	raw := []byte(`<familyset><family-list name="x-sst"><family><font>SST-Regular.ttf</font></family></family-list><alias name="sst" to="x-sst"/><family name="custom-icons"><font>Custom.ttf</font></family></familyset>`)
	families, aliases, err := inventoryDocument(raw, "/system/etc/fonts.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 2 || families[0].Name != "x-sst" || families[0].Role != "sans" || families[1].Role != "icons" || len(aliases) != 1 {
		t.Fatal(families, aliases)
	}
	doc, _ := parseXML(raw)
	restore := inheritFamilyNames(doc.Children[0])
	restore()
	if err = validateAndroidFontXML(doc); err != nil {
		t.Fatal(err)
	}
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	p.Coverage = &CoverageConfig{AutoFamilies: true, EngineCompatibility: true}
	if _, err = s.Stage(DefaultProfile()); err != nil {
		t.Fatal(err)
	}
	c := compiler{s, t.TempDir(), map[string]string{}, map[string]string{}, map[string]bool{}}
	out, _, err := c.compileXML("/system/etc/font_fallback.xml", []byte(`<familyset><family-list name="sans-serif"><family><font>Stock.ttf</font></family></family-list><family lang="ja"><font>Stock.ttf</font></family></familyset>`), p)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := parseXML(out)
	if err = validateAndroidFontXML(parsed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(`lang="zh-Hans,zh-Hant"`)) { // The original generic route is retained for old profiles; split routes must exist too.
		if !bytes.Contains(out, []byte(`lang="zh-Hans"`)) || !bytes.Contains(out, []byte(`lang="zh-Hant"`)) {
			t.Fatal("missing whitespace-parser compatible routes")
		}
	}
}

func TestUnicodeScopeAndVariationBounds(t *testing.T) {
	for _, item := range []struct {
		code  uint32
		scope string
		yes   bool
	}{
		{0x0628, "script:Arab", true}, {0x0628, "script:Latn", false}, {0x4e2d, "script:Hani", true},
		{0x3042, "script:Hani", false}, {0xE000, "script:Hani", false}, {0x1f600, "script:Hani", false},
		{'A', "mask:script:Hani", true}, {' ', "mask:script:Hani", true}, {0x4e2d, "mask:script:Hani", false},
		{'1', "mask:emoji", true}, {0xA9, "mask:emoji", true},
	} {
		if scopeKeeps(item.code, item.scope) != item.yes {
			t.Fatal(item)
		}
	}
	tables := tables(fontData(fixtureFont(false)), 0)
	sub := make([]byte, 33)
	put16(sub, 0, 14)
	put32(sub, 2, len(sub))
	put32(sub, 6, 1)
	sub[10] = 0
	sub[11] = 0xfe
	sub[12] = 0x0f
	put32(sub, 13, 21)
	put32(sub, 21, 2)
	copy(sub[25:], []byte{0, 0, 0x41, 0, 0, 0x4e, 0x2d, 0})
	plain := tables["cmap"]
	combined := make([]byte, 20)
	put16(combined, 2, 2)
	put16(combined, 4, 3)
	put16(combined, 6, 10)
	put32(combined, 8, 20)
	put16(combined, 12, 0)
	put16(combined, 14, 5)
	put32(combined, 16, 20+len(plain)-12)
	combined = append(combined, plain[12:]...)
	combined = append(combined, sub...)
	tables["cmap"] = combined
	filtered := fontData(filteredVariationCmap(tables, "script:Hani"))
	if filtered.u32(21) != 1 || !bytes.Equal(filtered[25:], []byte{0, 0x4e, 0x2d, 0}) {
		t.Fatalf("UVS scope filter: %x", filtered)
	}
	// Overlapping ranges must fail before scalar expansion.
	copy(combined[len(combined)-4:], []byte{0, 0, 0x41, 255})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("accepted overlapping UVS ranges")
			}
		}()
		filteredVariationCmap(tables, "all")
	}()
}

func TestCoverageRejectsNestedChainsAndPreservesRuleReferences(t *testing.T) {
	s, a, b := fixtureStore(t)
	p := variableProfile(a)
	main := p.Slots["chinese"]
	fallback := Slot{Mode: "static", MissingWeightPolicy: "nearest", Faces: []FaceRef{{AssetID: b.ID, Weight: 400, Style: "normal"}}}
	fallback.Fallbacks = []Slot{fallback}
	main.Fallbacks = []Slot{fallback}
	p.Slots["chinese"] = main
	if _, err := s.Validate(p); err == nil {
		t.Fatal("nested fallback accepted")
	}
	p = DefaultProfile()
	p.Coverage = &CoverageConfig{Rules: []CoverageRule{{ID: "han", Role: "serif", Scripts: []string{"Hani"}, Font: variableProfile(a).Slots["chinese"]}}}
	if _, err := s.Validate(p); err == nil {
		t.Fatal("unscoped Han language accepted")
	}
	p.Coverage.Rules[0].Languages = []string{"zh-Hans"}
	preset, err := s.SavePreset("", p)
	if err != nil {
		t.Fatal(err)
	}
	_ = preset
	if _, err = s.Handle(Request{Op: "delete_font", ID: a.ID}); err == nil {
		t.Fatal("advanced font reference deleted")
	}
}

func TestCoverageReportDoesNotStageOrSave(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	if _, err := s.Stage(DefaultProfile()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.data("pending"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.CoverageReport(p, "中文Aa", "zh-Hans", "sans-serif")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.data("pending"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("report staged a profile", err)
	}
	if report.Verification != "static-coverage-prediction" || report.AppRenderingVerified || len(report.Sample) != 4 {
		t.Fatal(report)
	}
}

func TestProductCoverageKeepsAllRulesAndStockSupplement(t *testing.T) {
	s, a, _ := fixtureStore(t)
	raw := []byte(`<fonts-modification><family name="x-sst" customizationType="new-named-family"><font>SST-Regular.ttf</font></family></fonts-modification>`)
	if err := writeAtomic(s.rom("/product/etc/fonts_customization.xml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(s.rom("/product/fonts/SST-Regular.ttf"), fixtureFont(false), 0600); err != nil {
		t.Fatal(err)
	}
	p := variableProfile(a)
	p.Slots["latinSans"] = p.Slots["chinese"]
	p.Coverage = &CoverageConfig{AutoFamilies: true, EngineCompatibility: true, Rules: []CoverageRule{
		{ID: "han", Role: "sans", Scripts: []string{"Hani"}, Languages: []string{"zh-Hans"}, Families: []string{"x-sst"}, Font: p.Slots["chinese"]},
		{ID: "latin", Role: "sans", Scripts: []string{"Latn"}, Families: []string{"x-sst"}, Font: p.Slots["latinSans"]},
	}}
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	var system []byte
	for _, target := range g.Targets {
		if target.Path == "/system/etc/fonts.xml" {
			system, err = os.ReadFile(s.data("generations", g.ID, target.File))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if !bytes.Contains(system, []byte(`fallbackFor="x-sst"`)) {
		t.Fatal("product fallback not routed")
	}
	files, _, err := inventoryDocument(system, "/system/etc/fonts.xml")
	if err != nil {
		t.Fatal(err)
	}
	routes := 0
	for _, family := range files {
		for _, name := range family.FallbackFor {
			if name == "x-sst" {
				routes++
			}
		}
	}
	if routes < 7 {
		t.Fatalf("expected both rules, compatibility locales and stock: %d", routes)
	}
}
