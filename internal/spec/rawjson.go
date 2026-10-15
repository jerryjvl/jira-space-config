package spec

import (
	"fmt"
	"strconv"
)

// JSONValue turns a raw value (as anyValue gives it) into the JSON value it
// stands for: scalars by their YAML tag, mappings as objects.
func JSONValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case ScalarValue:
		switch x.Tag {
		case "!!null":
			return nil
		case "!!bool":
			return x.Value == "true"
		case "!!int", "!!float":
			if n, err := strconv.ParseFloat(x.Value, 64); err == nil {
				return n
			}
		}
		return x.Value
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = JSONValue(item)
		}
		return out
	case []KVAny:
		out := make(map[string]any, len(x))
		for _, kv := range x {
			out[kv.Key] = JSONValue(kv.Value)
		}
		return out
	}
	// checkcover:unreachable anyValue and the reader give only the forms above.
	panic(fmt.Sprintf("spec.JSONValue: unexpected %T", v))
}
