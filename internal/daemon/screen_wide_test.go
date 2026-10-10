package daemon

import (
	"strings"
	"testing"
)

func BenchmarkScreenApplyCJK(b *testing.B) {
	data := []byte(strings.Repeat("日本語のテキストです。abc def 中文字符\r\n", 200))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		s := newScreenSized(80, 24)
		s.apply(data)
	}
}

func BenchmarkScreenApplyASCII(b *testing.B) {
	data := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog 0123456789\r\n", 200))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		s := newScreenSized(80, 24)
		s.apply(data)
	}
}
