package main

import (
	"fmt"
)

type nullWriterAt struct {
	bytesRead uint64
}

func newNullWriterAt() *nullWriterAt {
	w := &nullWriterAt{bytesRead: 0}
	return w
}

func (w *nullWriterAt) WriteAt(p []byte, off int64) (int, error) {
	w.bytesRead += uint64(len(p))
	return len(p), nil
}

func ByteRateSI(b float64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%.1f B/s", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB/s", b/float64(div), "kMGTPE"[exp])
}
