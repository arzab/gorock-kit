package rockconfig

import (
	"fmt"
	"os"
	"strings"
)

// expandEnvTree expands ${VAR} references in every string value of a decoded
// config tree. Map keys are left untouched. Expansion happens after decoding,
// so an env value can never change the structure of the file.
func expandEnvTree(node interface{}, path string) (interface{}, error) {
	switch v := node.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, child := range v {
			e, err := expandEnvTree(child, joinPath(path, k))
			if err != nil {
				return nil, err
			}
			out[k] = e
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, child := range v {
			e, err := expandEnvTree(child, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	case string:
		s, err := expandEnv(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return s, nil
	default:
		return node, nil
	}
}

// expandEnv replaces ${VAR} and ${VAR:-fallback} in s.
//
//   - ${VAR}            value of VAR; error if VAR is unset
//   - ${VAR:-fallback}  value of VAR, or fallback if VAR is unset or empty
//   - $${              literal "${"
//   - any other $       left as is (so "pa$$word" and bcrypt hashes survive)
func expandEnv(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "$${") {
			b.WriteString("${")
			i += 3
			continue
		}
		if !strings.HasPrefix(s[i:], "${") {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated ${ in %q", s)
		}
		expr := s[i+2 : i+2+end]
		name, fallback, hasFallback := strings.Cut(expr, ":-")
		if !validEnvName(name) {
			return "", fmt.Errorf("invalid env reference ${%s}", expr)
		}
		val, set := os.LookupEnv(name)
		switch {
		case hasFallback && val == "":
			val = fallback
		case !set:
			return "", fmt.Errorf("env var %s is not set (use ${%s:-default} for an optional value)", name, name)
		}
		b.WriteString(val)
		i += 2 + end + 1
	}
	return b.String(), nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
