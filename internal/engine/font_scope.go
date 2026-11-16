package engine

import (
	"sort"
	"strings"
	"unicode"
)

func rangeValue(c uint32, ranges []unicodeScriptRange) string {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= c })
	if i < len(ranges) && ranges[i].lo <= c {
		return ranges[i].scripts
	}
	return ""
}

func knownScript(script string) bool { _, ok := scriptNames[script]; return ok }
func privateUse(c uint32) bool {
	return c >= 0xE000 && c <= 0xF8FF || c >= 0xF0000 && c <= 0xFFFFD || c >= 0x100000 && c <= 0x10FFFD
}
func emojiCode(c uint32) bool { return rangeValue(c, unicodeEmoji[:]) != "" }

func scopeKeeps(c uint32, scope string) bool {
	if strings.HasPrefix(scope, "mask:") {
		selected := strings.TrimPrefix(scope, "mask:")
		// Emoji property includes ASCII keycap bases and text-presentation
		// symbols. Keep those in text primaries; Minikin uses sequence/style.
		if selected == "emoji" {
			return true
		}
		if strings.HasPrefix(selected, "script:") {
			value := rangeValue(c, unicodeScriptExtensions[:])
			if value == "" {
				value = rangeValue(c, unicodeScripts[:])
			}
			for _, script := range strings.Fields(value) {
				for _, wanted := range strings.Split(strings.TrimPrefix(selected, "script:"), ",") {
					if script == wanted {
						return false
					}
				}
			}
			return true
		}
		return !scopeKeeps(c, selected)
	}
	if strings.HasPrefix(scope, "without:") {
		return !scopeKeeps(c, strings.TrimPrefix(scope, "without:"))
	}
	switch scope {
	case "all":
		return true
	case "cjk":
		return isCJK(c)
	case "latin":
		return isLatin(c)
	case "without-cjk":
		return !isCJK(c)
	case "emoji":
		return emojiCode(c) || c == 0x200D || c == 0xFE0E || c == 0xFE0F || c >= 0xE0020 && c <= 0xE007F
	case "private-use":
		return privateUse(c)
	case "symbols":
		return unicode.IsSymbol(rune(c)) && !emojiCode(c)
	}
	if !strings.HasPrefix(scope, "script:") {
		return false
	}
	if privateUse(c) || emojiCode(c) && c > 0x7F {
		return false
	}
	wanted := strings.Split(strings.TrimPrefix(scope, "script:"), ",")
	value := rangeValue(c, unicodeScriptExtensions[:])
	if value == "" {
		value = rangeValue(c, unicodeScripts[:])
	}
	for _, script := range strings.Fields(value) {
		for _, w := range wanted {
			if w == script {
				return true
			}
		}
	}
	// Common punctuation, spacing and inherited combining marks must travel with
	// a script. Symbols and private-use/emoji are deliberately separate roles.
	return (value == "Zyyy" || value == "Zinh") && (unicode.IsPunct(rune(c)) || unicode.IsSpace(rune(c)) || unicode.IsMark(rune(c)))
}

func validScope(scope string) bool {
	if strings.HasPrefix(scope, "mask:") {
		return validScope(strings.TrimPrefix(scope, "mask:")) && !strings.HasPrefix(strings.TrimPrefix(scope, "mask:"), "mask:")
	}
	if strings.HasPrefix(scope, "without:") {
		return validScope(strings.TrimPrefix(scope, "without:")) && !strings.HasPrefix(strings.TrimPrefix(scope, "without:"), "without:")
	}
	switch scope {
	case "all", "cjk", "latin", "without-cjk", "emoji", "symbols", "private-use":
		return true
	}
	if !strings.HasPrefix(scope, "script:") {
		return false
	}
	for _, script := range strings.Split(strings.TrimPrefix(scope, "script:"), ",") {
		if !knownScript(script) {
			return false
		}
	}
	return true
}

func fontHasScope(b []byte, index int, scope string) (yes bool, err error) {
	defer fontRecover(&err)
	offs := offsets(fontData(b))
	if index < 0 || index >= len(offs) {
		panic("集合索引错误")
	}
	for c := range charmap(tables(fontData(b), offs[index])) {
		if scopeKeeps(c, scope) {
			return true, nil
		}
	}
	return false, nil
}

func faceCoverage(f *Face, cm map[uint32]uint16, t map[string]fontData) {
	seen := map[string]bool{}
	for c := range cm {
		value := rangeValue(c, unicodeScriptExtensions[:])
		if value == "" {
			value = rangeValue(c, unicodeScripts[:])
		}
		for _, script := range strings.Fields(value) {
			if script != "Zyyy" && script != "Zinh" {
				seen[script] = true
			}
		}
		f.Symbols = f.Symbols || scopeKeeps(c, "symbols")
		f.PrivateUse = f.PrivateUse || privateUse(c)
		f.Emoji = f.Emoji || emojiCode(c)
	}
	for script := range seen {
		f.Scripts = append(f.Scripts, script)
	}
	sort.Strings(f.Scripts)
	f.GlyphCount = len(cm)
	f.Shaping = len(t["GSUB"]) != 0 || len(t["GPOS"]) != 0
	for _, format := range []string{"COLR", "CBDT", "sbix", "SVG "} {
		if len(t[format]) != 0 {
			f.ColorFormats = append(f.ColorFormats, strings.TrimSpace(format))
		}
	}
	for i := 0; i < t["cmap"].u16(2); i++ {
		if t["cmap"].u16(t["cmap"].u32(8+i*8)) == 14 {
			f.VariationSequences = true
		}
	}
	// Check actual printable ASCII advances rather than trusting a name or flag.
	metrics := t["hhea"].u16(34)
	width := -1
	count := 0
	same := true
	if metrics > 0 {
		for c := uint32(0x21); c <= 0x7E; c++ {
			if g := cm[c]; g != 0 {
				i := min(int(g), metrics-1)
				w := t["hmtx"].u16(i * 4)
				if width >= 0 && width != w {
					same = false
				}
				width = w
				count++
			}
		}
	}
	f.Monospace = same && count >= 20
}
