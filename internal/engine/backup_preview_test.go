package engine

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func backupFixture(t *testing.T) (*Store, string, Profile) {
	t.Helper()
	s, a, _ := fixtureStore(t)
	s.Downloads = t.TempDir()
	p := variableProfile(a)
	if _, err := s.SavePreset("", p); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(s.data("draft.json"), p); err != nil {
		t.Fatal(err)
	}
	out, err := s.Backup()
	if err != nil {
		t.Fatal(err)
	}
	return s, out.(map[string]any)["path"].(string), p
}

func restoreInto(t *testing.T, s *Store, path string) map[string]int {
	t.Helper()
	result, err := s.prepareRestore(Request{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := s.mergeRestore(result.(map[string]any)["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return merged.(map[string]int)
}

func TestBackupRoundTripAndMergePreservesState(t *testing.T) {
	s, path, p := backupFixture(t)
	target := &Store{Root: filepath.Join(t.TempDir(), "data"), Downloads: s.Downloads}
	if err := os.MkdirAll(target.Root, 0700); err != nil {
		t.Fatal(err)
	}
	draft := DefaultProfile()
	if err := writeJSON(target.data("draft.json"), draft); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"active", "pending"} {
		if err := os.WriteFile(target.data(key), []byte("keep-"+key), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := restoreInto(t, target, path)
	if out["fonts"] != 2 || out["presets"] != 1 {
		t.Fatal(out)
	}
	items, err := target.Presets()
	if err != nil || len(items) != 1 || !sameProfile(items[0].Profile, p) {
		t.Fatal(items, err)
	}
	if _, err = target.Validate(items[0].Profile); err != nil {
		t.Fatal(err)
	}
	again := restoreInto(t, target, path)
	if again["fonts"] != 0 || again["presets"] != 0 {
		t.Fatal(again)
	}
	var got Profile
	if err = readJSON(target.data("draft.json"), &got); err != nil || !sameProfile(got, draft) {
		t.Fatal(got, err)
	}
	for _, key := range []string{"active", "pending"} {
		b, err := os.ReadFile(target.data(key))
		if err != nil || string(b) != "keep-"+key {
			t.Fatal(key, err)
		}
	}
}

func TestRestoreConflictAndPresetDeleteProtection(t *testing.T) {
	s, path, p := backupFixture(t)
	items, _ := s.Presets()
	changed := p
	changed.Name = "Test"
	changed.Slots = map[string]Slot{}
	for k, v := range p.Slots {
		changed.Slots[k] = v
	}
	v := changed.Slots["chinese"]
	v.WeightMap = map[string]float64{}
	for k, w := range p.Slots["chinese"].WeightMap {
		v.WeightMap[k] = w
	}
	v.WeightMap["400"] = 475
	changed.Slots["chinese"] = v
	if _, err := s.SavePreset(items[0].ID, changed); err != nil {
		t.Fatal(err)
	}
	if got := restoreInto(t, s, path); got["presets"] != 1 {
		t.Fatal(got)
	}
	if got := restoreInto(t, s, path); got["presets"] != 0 {
		t.Fatal("duplicate conflict copy", got)
	}
	if err := writeJSON(s.data("draft.json"), DefaultProfile()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Handle(Request{Op: "delete_font", ID: p.Slots["chinese"].AssetID}); err == nil {
		t.Fatal("deleted referenced font")
	}
}

func TestRestoreRejectsArchiveTampering(t *testing.T) {
	s, path, _ := backupFixture(t)
	original, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	for _, bad := range []string{"extra", "corrupt", "duplicate", "symlink"} {
		t.Run(bad, func(t *testing.T) {
			dst := filepath.Join(s.Downloads, bad+".zip")
			f, err := os.Create(dst)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for _, entry := range original.File {
				r, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				var b bytes.Buffer
				b.ReadFrom(r)
				r.Close()
				data := b.Bytes()
				if bad == "corrupt" && entry.Name != "manifest.json" {
					data[0] ^= 1
				}
				h := &zip.FileHeader{Name: entry.Name, Method: zip.Store}
				if bad == "symlink" && entry.Name != "manifest.json" {
					h.SetMode(os.ModeSymlink | 0777)
				}
				w, err := z.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				w.Write(data)
			}
			if bad == "extra" {
				w, _ := z.Create("../../outside")
				w.Write([]byte("bad"))
			}
			if bad == "duplicate" {
				w, _ := z.Create("manifest.json")
				w.Write([]byte("{}"))
			}
			z.Close()
			f.Close()
			if _, err = s.prepareRestore(Request{Path: dst}); err == nil {
				t.Fatal("accepted bad archive")
			}
		})
	}
}

func TestRestoreRecoveryRemovesOnlyUncommittedAdditions(t *testing.T) {
	s, a, _ := fixtureStore(t)
	items := []Preset{{ID: token(), Profile: DefaultProfile()}}
	b, _ := catalogBytes(items)
	added := digest([]byte("added")) + ".font"
	if err := os.WriteFile(s.data("library", added), []byte("added"), 0600); err != nil {
		t.Fatal(err)
	}
	j := restoreJournal{digest(b), []string{added}}
	if err := writeJSON(s.data("restore-journal.json"), j); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverRestore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.data("library", added)); !os.IsNotExist(err) {
		t.Fatal("uncommitted file retained")
	}
	if _, err := os.Stat(s.data("library", a.ID+".font")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.data("library", added), []byte("added"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(s.data("restore-journal.json"), j); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(s.data("presets.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverRestore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.data("library", added)); err != nil {
		t.Fatal("committed addition lost", err)
	}
}

func TestPreviewBytesAndBounds(t *testing.T) {
	s, a, _ := fixtureStore(t)
	if _, err := s.preparePreview(a.ID, 100); err == nil {
		t.Fatal("invalid face accepted")
	}
	out, err := s.preparePreview(a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	session := out.(map[string]any)
	token := session["token"].(string)
	var all []byte
	for offset := int64(0); offset < int64(session["size"].(int)); {
		r, err := s.readPreview(token, offset)
		if err != nil {
			t.Fatal(err)
		}
		chunk := r.(map[string]any)
		b, err := base64.StdEncoding.DecodeString(chunk["chunk"].(string))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, b...)
		offset = chunk["offset"].(int64)
	}
	faces, err := ParseFont(all)
	if err != nil || len(faces) != 1 || len(faces[0].Axes) == 0 {
		t.Fatal(faces, err)
	}
	if _, err = s.readPreview(token, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err = s.readPreview(token, int64(len(all)+1)); err == nil {
		t.Fatal("oversized offset accepted")
	}
	if _, err = s.Handle(Request{Op: "preview_release", Token: token}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.data("previews", token)); !os.IsNotExist(err) {
		t.Fatal("preview retained")
	}
}

func TestBackupUploadCannotFinishAsFont(t *testing.T) {
	s, path, _ := backupFixture(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Handle(Request{Op: "upload_begin", Kind: "backup", Size: int64(len(b))})
	if err != nil {
		t.Fatal(err)
	}
	token := r.(map[string]string)["token"]
	for offset := 0; offset < len(b); offset += 48 << 10 {
		end := offset + (48 << 10)
		if end > len(b) {
			end = len(b)
		}
		if _, err = s.Handle(Request{Op: "upload_chunk", Token: token, Offset: int64(offset), Chunk: base64.StdEncoding.EncodeToString(b[offset:end])}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Handle(Request{Op: "upload_finish", Token: token}); err == nil {
		t.Fatal("backup accepted as font")
	}
	if _, err = s.prepareRestore(Request{Token: token}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal([]byte(`{"op":"preview_prepare","faceIndex":1}`), &m); err != nil {
		t.Fatal(err)
	}
}
