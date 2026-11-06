package engine

import (
	"fmt"
	"syscall"
)

func requireSpace(path string, needed int64) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return err
	}
	if uint64(needed) > st.Bavail*uint64(st.Bsize) {
		return fmt.Errorf("存储空间不足，需要至少 %d MiB", (needed+(1<<20)-1)>>20)
	}
	return nil
}
