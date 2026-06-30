package spec

// Generic-map accessors. [Load] is decoding-agnostic, so values arrive as the
// interface{} shapes both gopkg.in/yaml.v3 and encoding/json produce. The two
// decoders differ on numbers (YAML yields int/int64, JSON yields float64), so
// asInt/asFloat accept either; everything else is identical between them.

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// onlyKey returns the single key of a one-entry map; callers guard len(m)==1.
func onlyKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return ""
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
