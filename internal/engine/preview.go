package engine

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func regularFile(path string, limit int64) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 0 || st.Size() > limit {
		return nil, fmt.Errorf("文件类型或大小无效")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		f.Close()
		return nil, fmt.Errorf("文件已变化")
	}
	return f, nil
}

func (s *Store) preparePreview(id string, index int) (any, error) {
	if _, _, err := s.lookup(id, index); err != nil {
		return nil, err
	}
	f, err := regularFile(s.data("library", id+".font"), MaxFontSize)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFontSize+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	if digest(b) != id {
		return nil, fmt.Errorf("字体内容校验失败")
	}
	// RewriteFont extracts the chosen TTC/OTC face and retains shaping/variation tables.
	b, err = RewriteFont(b, index, "all")
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFontSize {
		return nil, fmt.Errorf("预览字体过大")
	}
	dir, err := s.privateDir("previews")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	live := 0
	for _, entry := range entries {
		if !tokenPattern.MatchString(entry.Name()) {
			continue
		}
		st, e := entry.Info()
		if e != nil || !st.Mode().IsRegular() {
			continue
		}
		if time.Since(st.ModTime()) > time.Hour {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		} else {
			live++
		}
	}
	if live >= 24 {
		return nil, fmt.Errorf("预览队列已满，请关闭其他面板后重试")
	}
	t := token()
	if err = writeAtomic(s.data("previews", t), b, 0600); err != nil {
		return nil, err
	}
	return map[string]any{"token": t, "size": len(b)}, nil
}

func (s *Store) readPreview(t string, offset int64) (any, error) {
	if !tokenPattern.MatchString(t) || offset < 0 {
		return nil, fmt.Errorf("预览请求无效")
	}
	f, err := regularFile(s.data("previews", t), MaxFontSize)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if offset > st.Size() {
		return nil, fmt.Errorf("预览位置超出范围")
	}
	b := make([]byte, 48<<10)
	n, err := f.ReadAt(b, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return map[string]any{"chunk": base64.StdEncoding.EncodeToString(b[:n]), "offset": offset + int64(n), "size": st.Size()}, nil
}
