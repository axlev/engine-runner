module example.com/widget-oracle

go 1.26

require example.com/widget v0.0.0

// The oracle tests the very snapshot the reviewer sees, so it points at the
// same files rather than a copy. A copy would let the two drift, and then
// the "ground truth" would be about code nobody reviewed.
replace example.com/widget => ../prospective/reviewer/repository
