package roadrunner

import "fmt"

// normalizeCacheSize resolves the configured cache_size to a positive
// value safe to feed into mesi.NewMemoryCache.
//
//   - size <= 0 → DefaultCacheSize (10000). The empty config is the
//     documented "use the default" signal, so silent substitution here
//     is intentional (matches the documented default and the existing
//     caddy / apache behaviour for the same intent).
//   - 1 ≤ size ≤ MaxCacheSize → returned verbatim.
//   - size > MaxCacheSize → error. Rejecting here keeps the value
//     bounded before it reaches NewMemoryCache, matching the apache
//     MESI_MAX_CACHE_SIZE ceiling and closing the silent overflow
//     class flagged in the workflow anti-pattern ("Silent default
//     fallback in parsers" / out-of-range inputs flowing into
//     subsequent cache internals).
func normalizeCacheSize(size int) (int, error) {
	if size <= 0 {
		return DefaultCacheSize, nil
	}
	if size > MaxCacheSize {
		return 0, fmt.Errorf("cache_size %d exceeds max %d", size, MaxCacheSize)
	}
	return size, nil
}
