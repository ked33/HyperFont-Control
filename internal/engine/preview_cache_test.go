package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFastPreviewReusesBytesAndRejectsChangedSource(t *testing.T) {
	s, a, _ := fixtureStore(t)
	s.WebRoot = filepath.Join(t.TempDir(), "webroot")
	if err := os.Mkdir(s.WebRoot, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := s.prepareFastPreview(a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := first.(map[string]any)
	if r["cached"] != false {
		t.Fatal(r)
	}
	p := filepath.Join(s.WebRoot, r["url"].(string))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(s.data("library", a.ID+".font"))
	if err != nil || !bytes.Equal(source, b) {
		t.Fatal("standalone font changed", err)
	}
	second, err := s.prepareFastPreview(a.ID, 0)
	if err != nil || second.(map[string]any)["cached"] != true {
		t.Fatal(second, err)
	}
	if err = s.invalidatePreviewCache(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("deleted font cache retained")
	}
	// The private fallback hard link survives public cache eviction.
	if _, err = s.readPreview(r["token"].(string), 0); err != nil {
		t.Fatal(err)
	}
	source[0] ^= 1
	if err = os.WriteFile(s.data("library", a.ID+".font"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.prepareFastPreview(a.ID, 0); err == nil {
		t.Fatal("corrupt source accepted")
	}
}

func TestFastPreviewCollectionChoosesFaceAndRejectsCacheSymlink(t *testing.T) {
	s, _, _ := fixtureStore(t)
	s.WebRoot = t.TempDir()
	a, b := fixtureFont(false), fixtureFont(false)
	put16(tables(fontData(b), 0)["OS/2"], 4, 700)
	out := make([]byte, 20)
	copy(out, "ttcf")
	put32(out, 4, 65536)
	put32(out, 8, 2)
	for i, face := range [][]byte{a, b} {
		off := len(out)
		put32(out, 12+i*4, off)
		part := bytes.Clone(face)
		for j := 0; j < fontData(part).u16(4); j++ {
			p := 12 + j*16 + 8
			put32(part, p, fontData(part).u32(p)+off)
		}
		out = append(out, part...)
	}
	p := filepath.Join(t.TempDir(), "collection.ttc")
	if err := os.WriteFile(p, out, 0600); err != nil {
		t.Fatal(err)
	}
	asset, err := s.ImportFile(p, "Collection")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.prepareFastPreview(asset.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.WebRoot, r.(map[string]any)["url"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	faces, err := ParseFont(data)
	if err != nil || len(faces) != 1 || faces[0].Weight != 700 {
		t.Fatal(faces, err)
	}
	s.WebRoot = t.TempDir()
	if err = os.Symlink(t.TempDir(), filepath.Join(s.WebRoot, "preview-cache")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.prepareFastPreview(asset.ID, 0); err == nil {
		t.Fatal("cache symlink accepted")
	}
}
