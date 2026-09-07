//go:build ignore

package main

import "engine-runner-fixture/internal/paging"

// listPage renders one page of results, allocating exactly pageSize slots.
func listPage(items []string, offset, pageSize int) []string {
	page := paging.Paginate(items, offset, pageSize)
	if len(page) > pageSize {
		panic("paginate returned more than pageSize items")
	}
	return page
}
