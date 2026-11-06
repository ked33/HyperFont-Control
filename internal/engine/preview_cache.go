package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const previewCacheLimit int64 = 512 << 20

type previewCacheRecord struct {
	SourceSize     int64 `json:"sourceSize"`
	SourceModified int64 `json:"sourceModified"`
	Size           int64 `json:"size"`
	Used           int64 `json:"used"`
}

func (s *Store) previewCacheDirs() (string, string, error) {
	// WebRoot is set by the fixed CLI entrypoint, never by a Web request.
	if s.WebRoot == "" {
		return "", "", fmt.Errorf("WebUI 字体通道不可用")
	}
	st, err := os.Lstat(s.WebRoot)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("WebUI 目录无效")
	}
	public := filepath.Join(s.WebRoot, "preview-cache")
	if err = os.MkdirAll(public, 0700); err != nil {
		return "", "", err
	}
	st, err = os.Lstat(public)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("预览缓存目录无效")
	}
	private, err := s.privateDir("preview-cache")
	return public, private, err
}

func (s *Store) trimPreviewCache(public, private, keep string, needed int64) error {
	entries, err := os.ReadDir(public)
	if err != nil {
		return err
	}
	type cached struct {
		name       string
		size, used int64
	}
	items := []cached{}
	var total int64
	for _, entry := range entries {
		name := entry.Name()
		if !validPreviewCacheName(name) {
			continue
		}
		st, e := entry.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("预览缓存文件类型无效")
		}
		var rec previewCacheRecord
		_ = readJSON(filepath.Join(private, name+".json"), &rec)
		items = append(items, cached{name, st.Size(), rec.Used})
		total += st.Size()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].used < items[j].used })
	count := len(items)
	for _, item := range items {
		if item.name == keep {
			continue
		}
		if total+needed <= previewCacheLimit && count < 24 && time.Since(time.Unix(item.used, 0)) < time.Hour {
			continue
		}
		if err = removeIfExists(filepath.Join(public, item.name)); err != nil {
			return err
		}
		if err = removeIfExists(filepath.Join(private, item.name+".json")); err != nil {
			return err
		}
		total -= item.size
		count--
	}
	return nil
}

func validPreviewCacheName(name string) bool {
	if len(name) < 64+6 || !assetPattern.MatchString(name[:64]) {
		return false
	}
	for index := 0; index < 32; index++ {
		if name == fmt.Sprintf("%s-%d.ttf", name[:64], index) {
			return true
		}
	}
	return false
}

func (s *Store) prepareFastPreview(id string, index int) (any, error) {
	if _, _, err := s.lookup(id, index); err != nil {
		return nil, err
	}
	public, private, err := s.previewCacheDirs()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%d.ttf", id, index)
	source, err := regularFile(s.data("library", id+".font"), MaxFontSize)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	sourceStat, err := source.Stat()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(public, name)
	recordPath := filepath.Join(private, name+".json")
	var rec previewCacheRecord
	cached, cacheErr := os.Lstat(path)
	hit := readJSON(recordPath, &rec) == nil && cacheErr == nil && cached.Mode().IsRegular() && cached.Size() == rec.Size && rec.SourceSize == sourceStat.Size() && rec.SourceModified == sourceStat.ModTime().UnixNano()
	if !hit {
		b, e := io.ReadAll(io.LimitReader(source, MaxFontSize+1))
		if e != nil {
			return nil, e
		}
		if digest(b) != id {
			return nil, fmt.Errorf("字体内容校验失败")
		}
		// Standalone fonts are already import-validated. Avoid rebuilding their cmap
		// and all tables; collections still require extracting the selected face.
		if string(b[:4]) == "ttcf" {
			b, e = RewriteFont(b, index, "all")
			if e != nil {
				return nil, e
			}
		}
		if len(b) > MaxFontSize {
			return nil, fmt.Errorf("预览字体过大")
		}
		if err = s.trimPreviewCache(public, private, name, int64(len(b))); err != nil {
			return nil, err
		}
		if err = requireSpace(public, int64(len(b))+(1<<20)); err != nil {
			return nil, err
		}
		if err = writeAtomic(path, b, 0600); err != nil {
			return nil, err
		}
		rec = previewCacheRecord{SourceSize: sourceStat.Size(), SourceModified: sourceStat.ModTime().UnixNano(), Size: int64(len(b))}
	} else if err = s.trimPreviewCache(public, private, name, 0); err != nil {
		return nil, err
	}
	rec.Used = time.Now().Unix()
	if err = writeJSON(recordPath, rec); err != nil {
		return nil, err
	}
	// A private hard link keeps the fallback bytes alive if cache eviction occurs
	// while a manager is loading this preview. Old hosts can use the old protocol.
	dir, err := s.privateDir("previews")
	if err != nil {
		return nil, err
	}
	if err = s.cleanupPreviews(dir); err != nil {
		return nil, err
	}
	// Cached content may be older than the fallback expiry. Refresh the inode
	// timestamp so a later request cannot reap this active lease immediately.
	now := time.Now()
	if err = os.Chtimes(path, now, now); err != nil {
		return nil, err
	}
	idToken := token()
	if err = os.Link(path, filepath.Join(dir, idToken)); err != nil {
		return nil, err
	}
	return map[string]any{"token": idToken, "size": rec.Size, "url": "preview-cache/" + name, "cached": hit}, nil
}

func (s *Store) cleanupPreviews(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	live := 0
	for _, entry := range entries {
		if !tokenPattern.MatchString(entry.Name()) {
			continue
		}
		st, e := entry.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			continue
		}
		if time.Since(st.ModTime()) > time.Hour {
			if e = os.Remove(filepath.Join(dir, entry.Name())); e != nil {
				return e
			}
		} else {
			live++
		}
	}
	if live >= 24 {
		return fmt.Errorf("预览队列已满，请关闭其他面板后重试")
	}
	return nil
}

func (s *Store) invalidatePreviewCache(id string) error {
	if s.WebRoot == "" {
		return nil
	}
	public, private, err := s.previewCacheDirs()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(public)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if validPreviewCacheName(name) && strings.HasPrefix(name, id+"-") {
			if err = removeIfExists(filepath.Join(public, name)); err != nil {
				return err
			}
			if err = removeIfExists(filepath.Join(private, name+".json")); err != nil {
				return err
			}
		}
	}
	return nil
}
