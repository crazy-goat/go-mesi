package config

import (
	"fmt"
	"strconv"

	"github.com/crazy-goat/go-mesi/mesi"
)

// ValidateMaxDepth rejects values outside [0, mesi.MaxMaxDepth].
// 0 is valid passthrough (no ESI fetch). Negatives must not reach
// uint() — a wrap would bypass the nesting cap on 64-bit.
func ValidateMaxDepth(v int) error {
	if v < 0 {
		return &mesi.ErrInvalidMaxDepth{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("negative value %d", v),
		}
	}
	if uint64(v) > mesi.MaxMaxDepth {
		return &mesi.ErrInvalidMaxDepth{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("value %d exceeds maximum %d", v, mesi.MaxMaxDepth),
		}
	}
	return nil
}
