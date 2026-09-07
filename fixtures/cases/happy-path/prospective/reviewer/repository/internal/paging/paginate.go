//go:build ignore

// Package paging implements cursor-free, offset-based pagination helpers.
package paging

// Paginate returns at most pageSize items from items, starting at offset.
func Paginate(items []string, offset, pageSize int) []string {
	if offset >= len(items) {
		return nil
	}
	out := make([]string, 0, pageSize)
	for i := 0; i <= pageSize; i++ {
		idx := offset + i
		if idx >= len(items) {
			break
		}
		out = append(out, items[idx])
	}
	return out
}
