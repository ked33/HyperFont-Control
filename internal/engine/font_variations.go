package engine

// Preserve Unicode variation sequences whose base characters survive filtering.
// Glyph indexes remain unchanged, so outlines and shaping/color tables stay valid.
func filteredVariationCmap(t map[string]fontData, scope string) []byte {
	b := t["cmap"]
	for i := 0; i < b.u16(2); i++ {
		offset := b.u32(8 + i*8)
		if b.u16(offset) != 14 {
			continue
		}
		sub := b.cut(offset, b.u32(offset+2))
		count := sub.u32(6)
		if count > 256 {
			panic("变体选择器记录过多")
		}
		sub.cut(10, count*11)
		type record struct {
			selector              int
			defaults, nondefaults []byte
		}
		records := []record{}
		previousSelector := -1
		work := 0
		for j := 0; j < count; j++ {
			p := 10 + j*11
			selector := int(sub[p])<<16 | int(sub[p+1])<<8 | int(sub[p+2])
			if !(selector >= 0xFE00 && selector <= 0xFE0F || selector >= 0xE0100 && selector <= 0xE01EF) {
				panic("变体选择器无效")
			}
			if selector <= previousSelector {
				panic("变体选择器重复或倒序")
			}
			previousSelector = selector
			r := record{selector: selector}
			if off := sub.u32(p + 3); off != 0 {
				n := sub.u32(off)
				if n > 1114112 {
					panic("变体范围过多")
				}
				if off < 10+count*11 {
					panic("变体范围偏移无效")
				}
				sub.cut(off+4, n*4)
				r.defaults = make([]byte, 4)
				previousEnd := -1
				for k := 0; k < n; k++ {
					q := off + 4 + k*4
					start := uint32(sub[q])<<16 | uint32(sub[q+1])<<8 | uint32(sub[q+2])
					end := start + uint32(sub[q+3])
					if end > 0x10FFFF || int(start) <= previousEnd || start <= 0xDFFF && end >= 0xD800 {
						panic("变体范围越界")
					}
					previousEnd = int(end)
					work += int(end-start) + 1
					if work > 4*1114112 {
						panic("变体映射总量过大")
					}
					// Preserve compact ranges instead of expanding every scalar.
					runStart := int64(-1)
					for c := start; c <= end; c++ {
						if scopeKeeps(c, scope) {
							if runStart < 0 {
								runStart = int64(c)
							}
						} else if runStart >= 0 {
							r.defaults = append(r.defaults, byte(runStart>>16), byte(runStart>>8), byte(runStart), byte(int64(c)-runStart-1))
							runStart = -1
						}
					}
					if runStart >= 0 {
						r.defaults = append(r.defaults, byte(runStart>>16), byte(runStart>>8), byte(runStart), byte(int64(end)-runStart))
					}
				}
				put32(r.defaults, 0, (len(r.defaults)-4)/4)
				if len(r.defaults) == 4 {
					r.defaults = nil
				}
			}
			if off := sub.u32(p + 7); off != 0 {
				n := sub.u32(off)
				if n > 1114112 {
					panic("变体映射过多")
				}
				if off < 10+count*11 {
					panic("变体映射偏移无效")
				}
				sub.cut(off+4, n*5)
				work += n
				if work > 4*1114112 {
					panic("变体映射总量过大")
				}
				r.nondefaults = make([]byte, 4)
				previousCode := -1
				for k := 0; k < n; k++ {
					q := off + 4 + k*5
					raw := sub.cut(q, 5)
					c := uint32(raw[0])<<16 | uint32(raw[1])<<8 | uint32(raw[2])
					if c > 0x10FFFF || c >= 0xD800 && c <= 0xDFFF || int(c) <= previousCode || sub.u16(q+3) >= t["maxp"].u16(4) {
						panic("变体字形越界")
					}
					previousCode = int(c)
					if scopeKeeps(c, scope) {
						r.nondefaults = append(r.nondefaults, raw...)
					}
				}
				put32(r.nondefaults, 0, (len(r.nondefaults)-4)/5)
				if len(r.nondefaults) == 4 {
					r.nondefaults = nil
				}
			}
			if len(r.defaults)+len(r.nondefaults) > 0 {
				records = append(records, r)
			}
		}
		if len(records) == 0 {
			return nil
		}
		out := make([]byte, 10+len(records)*11)
		put16(out, 0, 14)
		put32(out, 6, len(records))
		for j, r := range records {
			p := 10 + j*11
			out[p] = byte(r.selector >> 16)
			out[p+1] = byte(r.selector >> 8)
			out[p+2] = byte(r.selector)
			if len(r.defaults) > 0 {
				put32(out, p+3, len(out))
				out = append(out, r.defaults...)
			}
			if len(r.nondefaults) > 0 {
				put32(out, p+7, len(out))
				out = append(out, r.nondefaults...)
			}
		}
		put32(out, 2, len(out))
		return out
	}
	return nil
}

func replacementCmap(t map[string]fontData, scope string) []byte {
	plain := makeCmap(charmap(t), scope)
	variation := filteredVariationCmap(t, scope)
	if len(variation) == 0 {
		return plain
	}
	out := make([]byte, 20)
	put16(out, 2, 2)
	put16(out, 4, 3)
	put16(out, 6, 10)
	put32(out, 8, 20)
	put16(out, 12, 0)
	put16(out, 14, 5)
	put32(out, 16, 20+len(plain)-12)
	out = append(out, plain[12:]...)
	out = append(out, variation...)
	return out
}
