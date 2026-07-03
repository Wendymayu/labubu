package storage

import "strconv"

// DeriveTokenBuckets extracts disjoint token buckets from an attribute map.
// total = input + output + cacheCreation + cacheRead (always; any self-reported
// gen_ai.usage.total_tokens is IGNORED). Assumes input_tokens is non-cached
// (cache reported in separate buckets). If a future agent reports input that
// already includes cache (OpenAI-style prompt_tokens), add a rule HERE to derive
// non-cached input before summing — this function is the single extension point.
func DeriveTokenBuckets(attrs map[string]string) (input, output, cacheCreation, cacheRead, total *uint32) {
	input = readUint32(attrs,
		"gen_ai.usage.input_tokens", "input_tokens", "llm.usage.input_tokens")
	output = readUint32(attrs,
		"gen_ai.usage.output_tokens", "output_tokens", "llm.usage.output_tokens")
	cacheCreation = readUint32(attrs,
		"gen_ai.usage.cache_creation_input_tokens", "cache_creation_tokens", "cache_creation_input_tokens")
	cacheRead = readUint32(attrs,
		"gen_ai.usage.cache_read_input_tokens", "cache_read_tokens", "cache_read_input_tokens")

	if input == nil && output == nil && cacheCreation == nil && cacheRead == nil {
		return
	}
	var sum uint32
	for _, p := range []*uint32{input, output, cacheCreation, cacheRead} {
		if p != nil {
			sum += *p
		}
	}
	total = &sum
	return
}

// readUint32 returns the first present, parseable attribute as *uint32.
func readUint32(attrs map[string]string, keys ...string) *uint32 {
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				nv := uint32(n)
				return &nv
			}
		}
	}
	return nil
}
