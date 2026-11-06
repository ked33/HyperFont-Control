package engine

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxBackupSize int64 = 4 << 30
const maxBackupEntries = 8192
const maxManifestSize = 8 << 20

type BackupManifest struct {
	Format    string             `json:"format"`
	Version   int                `json:"version"`
	Created   string             `json:"created"`
	Fonts     []Asset            `json:"fonts"`
	Presets   []Preset           `json:"presets"`
	Snapshots map[string]Profile `json:"snapshots"`
}
type restoreJournal struct {
	CatalogHash string   `json:"catalogHash"`
	Files       []string `json:"files"`
}

// Only private, direct children of the owned store may hold transient data.
func (s *Store) privateDir(name string) (string, error) {
	dir := s.data(name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("模块数据目录无效: %s", name)
	}
	return dir, nil
}

func (s *Store) Backup() (any, error) {
	fonts, err := s.Library()
	if err != nil {
		return nil, err
	}
	presets, err := s.Presets()
	if err != nil {
		return nil, err
	}
	m := BackupManifest{"hyperfont-control-backup", 1, time.Now().UTC().Format(time.RFC3339), fonts, presets, map[string]Profile{}}
	var draft Profile
	if err = readJSON(s.data("draft.json"), &draft); err == nil {
		m.Snapshots["draft"] = draft
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, key := range []string{"active", "pending"} {
		g, e := s.generation(key)
		if e != nil {
			return nil, e
		}
		if g != nil {
			m.Snapshots[key] = g.Profile
		}
	}
	if len(fonts)+1 > maxBackupEntries {
		return nil, fmt.Errorf("备份字体数量过多")
	}
	for _, p := range presets {
		if _, err = s.Validate(p.Profile); err != nil {
			return nil, err
		}
	}
	for _, p := range m.Snapshots {
		if _, err = s.Validate(p); err != nil {
			return nil, err
		}
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(manifest) > maxManifestSize {
		return nil, fmt.Errorf("备份清单过大")
	}
	total := int64(len(manifest))
	for _, a := range fonts {
		if a.Size < 12 || a.Size > MaxFontSize {
			return nil, fmt.Errorf("字体大小无效")
		}
		total += a.Size
	}
	if total > MaxBackupSize-(16<<20) {
		return nil, fmt.Errorf("备份超过 4 GiB 上限")
	}
	dir, err := s.downloads()
	if err != nil {
		return nil, err
	}
	if err = requireSpace(dir, total+(16<<20)); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".HyperFont-backup-*.tmp")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	z := zip.NewWriter(f)
	w, err := z.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(manifest); err != nil {
		return nil, err
	}
	for _, a := range fonts {
		if !assetPattern.MatchString(a.ID) {
			return nil, fmt.Errorf("字体 ID 无效")
		}
		src, e := regularFile(s.data("library", a.ID+".font"), MaxFontSize)
		if e != nil {
			return nil, e
		}
		w, e := z.CreateHeader(&zip.FileHeader{Name: "fonts/" + a.ID + ".font", Method: zip.Store})
		if e != nil {
			src.Close()
			return nil, e
		}
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(w, h), io.LimitReader(src, MaxFontSize+1))
		src.Close()
		if e != nil {
			return nil, e
		}
		if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.ID {
			return nil, fmt.Errorf("字体内容校验失败: %s", a.Label)
		}
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "HyperFont-backup-"+time.Now().Format("20060102-150405")+"-"+token()[:8]+".zip")
	if err = os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "fonts": len(fonts), "presets": len(presets)}, nil
}

func (s *Store) backupPath(r Request) (string, error) {
	if r.Token != "" {
		if !tokenPattern.MatchString(r.Token) {
			return "", fmt.Errorf("上传令牌无效")
		}
		var begin Request
		if err := readJSON(s.data("uploads", r.Token+".json"), &begin); err != nil {
			return "", err
		}
		if begin.Kind != "backup" {
			return "", fmt.Errorf("不是备份上传")
		}
		path := s.data("uploads", r.Token)
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Size() != begin.Size {
			return "", fmt.Errorf("备份尚未传完整")
		}
		return path, nil
	}
	dir, err := s.downloads()
	if err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(r.Path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("仅允许从 Download 读取备份")
	}
	return path, nil
}

func (s *Store) listBackups() (any, error) {
	dir, err := s.downloads()
	if err != nil {
		return nil, err
	}
	result := []DownloadFont{}
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(dir, path)
		if entry.IsDir() {
			if strings.Count(rel, string(os.PathSeparator)) >= 1 || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".zip") {
			return nil
		}
		st, e := entry.Info()
		if e != nil {
			return e
		}
		if st.Mode().IsRegular() && st.Size() <= MaxBackupSize {
			result = append(result, DownloadFont{Path: path, Name: rel, Size: st.Size(), Modified: st.ModTime().Unix()})
		}
		return nil
	})
	return result, err
}

func (s *Store) prepareRestore(r Request) (result any, err error) {
	path, err := s.backupPath(r)
	if err != nil {
		return nil, err
	}
	f, err := regularFile(path, MaxBackupSize)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return nil, err
	}
	if len(z.File) == 0 || len(z.File) > maxBackupEntries {
		return nil, fmt.Errorf("备份文件数量无效")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, entry := range z.File {
		if !entry.Mode().IsRegular() || entries[entry.Name] != nil {
			return nil, fmt.Errorf("备份包含重复或非普通文件")
		}
		if entry.UncompressedSize64 > uint64(MaxBackupSize) {
			return nil, fmt.Errorf("备份条目过大")
		}
		total += entry.UncompressedSize64
		if total > uint64(MaxBackupSize) {
			return nil, fmt.Errorf("解压内容超过 4 GiB")
		}
		entries[entry.Name] = entry
	}
	entry := entries["manifest.json"]
	if entry == nil || entry.UncompressedSize64 > maxManifestSize {
		return nil, fmt.Errorf("缺少或无效的备份清单")
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(reader, maxManifestSize+1))
	reader.Close()
	if err != nil {
		return nil, err
	}
	var m BackupManifest
	if err = strictJSON(b, &m); err != nil {
		return nil, err
	}
	if m.Format != "hyperfont-control-backup" || m.Version != 1 || len(m.Fonts)+1 != len(entries) {
		return nil, fmt.Errorf("不支持的备份格式或额外条目")
	}
	if len(m.Presets) > maxBackupEntries || len(m.Snapshots) > 3 {
		return nil, fmt.Errorf("方案数量无效")
	}
	seen := map[string]bool{}
	for _, a := range m.Fonts {
		if !assetPattern.MatchString(a.ID) || seen[a.ID] || a.Size < 12 || a.Size > MaxFontSize || len([]rune(a.Label)) > 200 {
			return nil, fmt.Errorf("备份字体记录无效")
		}
		seen[a.ID] = true
		entry := entries["fonts/"+a.ID+".font"]
		if entry == nil || entry.UncompressedSize64 != uint64(a.Size) {
			return nil, fmt.Errorf("备份字体缺失或大小不符")
		}
	}
	dir, err := s.privateDir("restores")
	if err != nil {
		return nil, err
	}
	// Expired sessions contain only module-generated, direct regular files.
	old, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range old {
		if tokenPattern.MatchString(item.Name()) {
			info, e := item.Info()
			if e == nil && info.IsDir() && time.Since(info.ModTime()) > 24*time.Hour {
				if e = s.cancelRestore(item.Name()); e != nil {
					return nil, e
				}
			}
		}
	}
	if err = requireSpace(s.Root, int64(total)+(16<<20)); err != nil {
		return nil, err
	}
	id := token()
	session := filepath.Join(dir, id)
	if err = os.Mkdir(session, 0700); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.cancelRestore(id)
		}
	}()
	staged := &Store{Root: session}
	for i, a := range m.Fonts {
		reader, e := entries["fonts/"+a.ID+".font"].Open()
		if e != nil {
			return nil, e
		}
		bytes, e := io.ReadAll(io.LimitReader(reader, MaxFontSize+1))
		reader.Close()
		if e != nil {
			return nil, e
		}
		if int64(len(bytes)) != a.Size || digest(bytes) != a.ID {
			return nil, fmt.Errorf("字体哈希不符: %s", a.Label)
		}
		faces, e := ParseFont(bytes)
		if e != nil {
			return nil, e
		}
		a.Faces = faces
		m.Fonts[i] = a
		if e = writeAtomic(staged.data("library", a.ID+".font"), bytes, 0600); e != nil {
			return nil, e
		}
		if e = writeJSON(staged.data("library", a.ID+".json"), a); e != nil {
			return nil, e
		}
	}
	seen = map[string]bool{}
	for _, p := range m.Presets {
		if !tokenPattern.MatchString(p.ID) || seen[p.ID] {
			return nil, fmt.Errorf("备份方案 ID 无效")
		}
		seen[p.ID] = true
		if _, err = staged.Validate(p.Profile); err != nil {
			return nil, err
		}
	}
	for k, p := range m.Snapshots {
		if k != "draft" && k != "active" && k != "pending" {
			return nil, fmt.Errorf("快照类型无效")
		}
		if _, err = staged.Validate(p); err != nil {
			return nil, err
		}
	}
	if err = writeJSON(filepath.Join(session, "manifest.json"), m); err != nil {
		return nil, err
	}
	return map[string]any{"token": id, "fonts": len(m.Fonts), "presets": len(m.Presets), "snapshots": len(m.Snapshots)}, nil
}

func (s *Store) cancelRestore(id string) error {
	if !tokenPattern.MatchString(id) {
		return fmt.Errorf("还原令牌无效")
	}
	dir := s.data("restores", id)
	for _, sub := range []string{"library", ""} {
		path := filepath.Join(dir, sub)
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("还原临时目录无效")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				return fmt.Errorf("还原临时目录包含意外子目录")
			}
			if err = os.Remove(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

// Recovery is run under the store lock before any request. Catalog publication
// commits a merge; a killed process before that point leaves only removable additions.
func (s *Store) recoverRestore() error {
	var j restoreJournal
	err := readJSON(s.data("restore-journal.json"), &j)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !assetPattern.MatchString(j.CatalogHash) {
		return fmt.Errorf("还原日志无效")
	}
	h, _ := hashFile(s.data("presets.json"))
	if h != j.CatalogHash {
		for _, name := range j.Files {
			id := strings.TrimSuffix(strings.TrimSuffix(name, ".font"), ".json")
			if !assetPattern.MatchString(id) || (name != id+".font" && name != id+".json") {
				return fmt.Errorf("还原日志路径无效")
			}
			if err = removeIfExists(s.data("library", name)); err != nil {
				return err
			}
		}
	}
	return removeIfExists(s.data("restore-journal.json"))
}

func (s *Store) mergeRestore(id string) (result any, err error) {
	if !tokenPattern.MatchString(id) {
		return nil, fmt.Errorf("还原令牌无效")
	}
	session := s.data("restores", id)
	var m BackupManifest
	if err = readJSON(filepath.Join(session, "manifest.json"), &m); err != nil {
		return nil, err
	}
	items, err := s.Presets()
	if err != nil {
		return nil, err
	}
	incoming := append([]Preset(nil), m.Presets...)
	for _, k := range []string{"draft", "active", "pending"} {
		if p, ok := m.Snapshots[k]; ok {
			incoming = append(incoming, Preset{Profile: p})
		}
	}
	added := 0
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range incoming {
		duplicate := false
		conflict := false
		for _, old := range items {
			if sameProfile(old.Profile, p.Profile) {
				duplicate = true
				break
			}
			if old.ID == p.ID || old.Profile.Name == p.Profile.Name {
				conflict = true
			}
		}
		if duplicate {
			continue
		}
		if conflict {
			original := []rune(p.Profile.Name)
			if len(original) > 65 {
				original = original[:65]
			}
			p.Profile.Name = string(original) + " · 还原副本"
		}
		// All imported IDs are local, collision-free IDs. Repeated restore is deduplicated by profile content below.
		if conflict {
			for _, old := range items {
				if sameProfile(old.Profile, p.Profile) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
		}
		p.ID = token()
		p.Created = now
		p.Updated = now
		items = append(items, p)
		added++
	}
	catalog, err := catalogBytes(items)
	if err != nil {
		return nil, err
	}
	if len(catalog) > maxManifestSize {
		return nil, fmt.Errorf("方案目录过大")
	}
	if _, err = s.privateDir("library"); err != nil {
		return nil, err
	}
	journal := restoreJournal{CatalogHash: digest(catalog), Files: []string{}}
	fontCount := 0
	for _, a := range m.Fonts {
		if !assetPattern.MatchString(a.ID) {
			return nil, fmt.Errorf("字体 ID 无效")
		}
		src := filepath.Join(session, "library", a.ID+".font")
		h, e := hashFile(src)
		if e != nil || h != a.ID {
			return nil, fmt.Errorf("暂存字体校验失败")
		}
		for _, suffix := range []string{".font", ".json"} {
			path := s.data("library", a.ID+suffix)
			st, e := os.Lstat(path)
			if os.IsNotExist(e) {
				journal.Files = append(journal.Files, a.ID+suffix)
				if suffix == ".font" {
					fontCount++
				}
				continue
			}
			if e != nil || !st.Mode().IsRegular() {
				return nil, fmt.Errorf("已有字体文件异常")
			}
			if suffix == ".font" {
				h, e = hashFile(path)
				if e != nil || h != a.ID {
					return nil, fmt.Errorf("已有字体损坏，请先处理后再还原")
				}
			}
		}
	}
	if err = writeJSON(s.data("restore-journal.json"), journal); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if e := s.recoverRestore(); e != nil {
				err = fmt.Errorf("%v；回滚失败: %w", err, e)
			}
		}
	}()
	for _, name := range journal.Files {
		if err = os.Rename(filepath.Join(session, "library", name), s.data("library", name)); err != nil {
			return nil, err
		}
	}
	// Persist moved directory entries before publishing the catalog commit record.
	if err = syncDirectory(s.data("library")); err != nil {
		return nil, err
	}
	for _, p := range items {
		if _, err = s.Validate(p.Profile); err != nil {
			return nil, err
		}
	}
	if err = writeAtomic(s.data("presets.json"), catalog, 0600); err != nil {
		return nil, err
	}
	if err = s.recoverRestore(); err != nil {
		return nil, err
	}
	_ = s.cancelRestore(id)
	return map[string]int{"fonts": fontCount, "presets": added}, nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
