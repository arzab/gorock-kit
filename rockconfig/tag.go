package rockconfig

import (
	"strings"
	"unicode"
)

// fieldTag holds parsed options from a config struct tag.
type fieldTag struct {
	name       string // key name in the config file
	ignore     bool
	omitempty  bool
	defaultVal string // value to use if field is missing/empty
	envVar     string // explicit env var name to read from
}

// parseFieldTag parses the raw config tag string for the given Go field name.
//
// Format: config:"[name][,opt[=val]]..."
//
//	config:""           → snake_case name, required
//	config:","          → snake_case name (explicit comma), required
//	config:",omitempty" → snake_case name, optional
//	config:",default=X" → snake_case name, default value X
//	config:",env=VAR"   → snake_case name, read from $VAR
//	config:"host"       → explicit name "host"
//	config:"-"          → ignore field entirely
//
// Note: "default=" must be the last option in the tag, as it consumes
// the rest of the string to allow commas inside default values.
func parseFieldTag(raw string, fieldName string) fieldTag {
	if raw == "-" {
		return fieldTag{ignore: true}
	}

	// Split only on the first comma to separate name from options string.
	name, opts, _ := strings.Cut(raw, ",")

	ft := fieldTag{}
	if name != "" {
		ft.name = name
	} else {
		ft.name = toSnakeCase(fieldName)
	}

	if opts == "" {
		return ft
	}

	// Parse options one by one, stopping at "default=" and consuming the rest
	// as the default value (allows commas inside default values).
	for opts != "" {
		var opt string
		if idx := strings.Index(opts, ","); idx >= 0 {
			opt, opts = opts[:idx], opts[idx+1:]
		} else {
			opt, opts = opts, ""
		}

		switch {
		case opt == "omitempty":
			ft.omitempty = true
		case strings.HasPrefix(opt, "env="):
			if name := strings.TrimPrefix(opt, "env="); name != "" {
				ft.envVar = name
			}
		case strings.HasPrefix(opt, "default="):
			// Consume the rest of the string as the default value,
			// so commas inside default values are preserved.
			ft.defaultVal = strings.TrimPrefix(opt, "default=")
			if opts != "" {
				ft.defaultVal += "," + opts
			}
			opts = ""
		}
	}

	return ft
}

// toSnakeCase converts a Go identifier to snake_case.
//
//	MyField    → my_field
//	HTTPServer → http_server
//	UserID     → user_id
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	var result []rune

	for i, r := range runes {
		if i == 0 {
			result = append(result, unicode.ToLower(r))
			continue
		}

		if unicode.IsUpper(r) {
			prev := runes[i-1]
			// Insert underscore when transitioning from lower→upper (fooBar)
			// or upper→upper+lower (HTTPServer → http_server, before S).
			needsUnderscore := unicode.IsLower(prev) ||
				(unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))
			if needsUnderscore {
				result = append(result, '_')
			}
		}

		result = append(result, unicode.ToLower(r))
	}

	return string(result)
}
