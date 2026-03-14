package rockconfig

import (
	"reflect"
)

// validate walks v and returns dot-separated paths of required fields that are empty.
func validate(v reflect.Value, path string) []string {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return []string{path}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()
	var issues []string

	for i := 0; i < t.NumField(); i++ {
		structField := t.Field(i)
		fieldVal := v.Field(i)

		// Skip unexported fields — they can never be set, so don't report them as missing.
		if !structField.IsExported() {
			continue
		}

		ft := parseFieldTag(structField.Tag.Get("config"), structField.Name)
		if ft.ignore || ft.defaultVal != "" {
			continue
		}

		fieldPath := path + "." + structField.Name

		// Dereference pointer.
		fv := fieldVal
		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				// omitempty: struct absent → OK. Required: report missing.
				if !ft.omitempty {
					issues = append(issues, fieldPath)
				}
				continue
			}
			fv = fv.Elem()
		}

		// Nested struct: always recurse if present, even with omitempty.
		// omitempty only allows the struct to be fully zero — if it has any
		// non-zero field, its children are validated normally.
		if fv.Kind() == reflect.Struct {
			if ft.omitempty && fv.IsZero() {
				continue // struct absent/zero, omitempty allows skipping
			}
			issues = append(issues, validate(fv, fieldPath)...)
			continue
		}

		// Scalar field with omitempty: skip empty check.
		if ft.omitempty {
			continue
		}

		if isEmpty(fieldVal) {
			issues = append(issues, fieldPath)
		}
	}

	return issues
}

// isEmpty reports whether v holds an empty/zero value that signals a missing config field.
// bool false is explicitly NOT considered empty — it is a valid value.
func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Map, reflect.Chan:
		return v.IsNil() || v.Len() == 0
	case reflect.Array:
		return v.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return false // false is a valid config value, never "missing"
	default:
		return false
	}
}
