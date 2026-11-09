package rockconfig

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"time"
)

// loader fills a struct from decoded config data and records problems.
// Paths in messages use config keys (e.g. "http.port"), as the user writes them.
type loader struct {
	opts    options
	missing []string // required fields not provided (merged in at the top level)
	unknown []string // keys present in the data that match no field
}

// populateStruct fills v from data.
//
// It returns whether any field received a value (from the file, an env var or
// a default) and the required fields that are missing. The caller decides
// whether those missing fields count: an absent optional section discards them.
func (l *loader) populateStruct(data map[string]interface{}, v reflect.Value, path string) (set bool, missing []string, err error) {
	t := v.Type()
	used := make(map[string]bool, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}

		ft := parseFieldTag(sf.Tag.Get("config"), sf.Name)
		if ft.ignore {
			continue
		}
		used[ft.name] = true
		fpath := joinPath(path, ft.name)
		required := !ft.omitempty && ft.defaultVal == ""

		// An explicit env binding has the highest priority.
		if ft.envVar != "" {
			if envVal, ok := os.LookupEnv(ft.envVar); ok {
				if err := setFromString(fv, envVal); err != nil {
					return false, nil, fmt.Errorf("%s (from env %s): %w", fpath, ft.envVar, err)
				}
				set = true
				continue
			}
		}

		raw, found := data[ft.name]
		if raw == nil {
			found = false // "key:" with no value (YAML null) counts as absent
		}

		// Nested struct or pointer to struct: recurse even when the section is
		// absent, so defaults and env bindings inside it still apply.
		if elemType, isPtr, ok := structTarget(fv); ok {
			var sub map[string]interface{}
			if found {
				m, isMap := raw.(map[string]interface{})
				if !isMap {
					return false, nil, fmt.Errorf("%s: expected an object, got %T", fpath, raw)
				}
				sub = m
			}
			target := fv
			if isPtr {
				target = reflect.New(elemType).Elem()
			}
			childSet, childMissing, err := l.populateStruct(sub, target, fpath)
			if err != nil {
				return false, nil, err
			}
			switch {
			case found || childSet:
				if isPtr {
					fv.Set(target.Addr())
				}
				missing = append(missing, childMissing...)
				set = true
			case ft.omitempty:
				// Absent optional section: nothing inside is required.
			case isPtr:
				missing = append(missing, fpath)
			default:
				missing = append(missing, childMissing...)
			}
			continue
		}

		if found {
			if s, isStr := raw.(string); isStr && s == "" && ft.defaultVal != "" {
				raw = ft.defaultVal // empty value falls back to the default
			}
			if err := l.setValue(fv, raw, fpath); err != nil {
				return false, nil, err
			}
			if required && isEmptyString(fv) {
				missing = append(missing, fpath+" (empty)")
			}
			set = true
			continue
		}

		if ft.defaultVal != "" {
			if err := setFromString(fv, ft.defaultVal); err != nil {
				return false, nil, fmt.Errorf("%s (default %q): %w", fpath, ft.defaultVal, err)
			}
			set = true
			continue
		}

		if required {
			missing = append(missing, fpath)
		}
	}

	var unknown []string
	for k := range data {
		if !used[k] {
			unknown = append(unknown, joinPath(path, k))
		}
	}
	sort.Strings(unknown)
	l.unknown = append(l.unknown, unknown...)

	return set, missing, nil
}

// structTarget reports whether fv is a struct or a pointer to a struct that
// should be populated from a nested object.
func structTarget(fv reflect.Value) (elem reflect.Type, isPtr bool, ok bool) {
	t := fv.Type()
	if t.Kind() == reflect.Struct {
		return t, false, true
	}
	if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
		return t.Elem(), true, true
	}
	return nil, false, false
}

func isEmptyString(v reflect.Value) bool {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Kind() == reflect.String && v.String() == ""
}

// setValue sets field to val, handling pointers, nested structs (e.g. inside
// slices and maps), slices and maps.
func (l *loader) setValue(field reflect.Value, val interface{}, path string) error {
	if field.Kind() == reflect.Ptr {
		if val == nil {
			return nil
		}
		elem := reflect.New(field.Type().Elem())
		if err := l.setValue(elem.Elem(), val, path); err != nil {
			return err
		}
		field.Set(elem)
		return nil
	}

	if field.Kind() == reflect.Struct {
		nested, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s: expected an object, got %T", path, val)
		}
		_, missing, err := l.populateStruct(nested, field, path)
		if err != nil {
			return err
		}
		l.missing = append(l.missing, missing...)
		return nil
	}

	switch field.Kind() {
	case reflect.Slice:
		return l.setSlice(field, val, path)
	case reflect.Map:
		return l.setMap(field, val, path)
	}

	if err := convertAndSet(field, val); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (l *loader) setSlice(field reflect.Value, val interface{}, path string) error {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("%s: expected an array, got %T", path, val)
	}
	slice := reflect.MakeSlice(field.Type(), rv.Len(), rv.Len())
	for i := 0; i < rv.Len(); i++ {
		if err := l.setValue(slice.Index(i), rv.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	field.Set(slice)
	return nil
}

func (l *loader) setMap(field reflect.Value, val interface{}, path string) error {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Map {
		return fmt.Errorf("%s: expected an object, got %T", path, val)
	}
	m := reflect.MakeMap(field.Type())
	keyType := field.Type().Key()
	valType := field.Type().Elem()
	for _, key := range rv.MapKeys() {
		kpath := fmt.Sprintf("%s.%v", path, key.Interface())
		mapKey := reflect.New(keyType).Elem()
		if err := convertAndSet(mapKey, key.Interface()); err != nil {
			return fmt.Errorf("%s: key: %w", kpath, err)
		}
		mapVal := reflect.New(valType).Elem()
		if err := l.setValue(mapVal, rv.MapIndex(key).Interface(), kpath); err != nil {
			return err
		}
		m.SetMapIndex(mapKey, mapVal)
	}
	field.Set(m)
	return nil
}

// setFromString sets field from a string value (used for defaults and env vars).
func setFromString(field reflect.Value, s string) error {
	if field.Kind() == reflect.Ptr {
		elem := reflect.New(field.Type().Elem())
		if err := setFromString(elem.Elem(), s); err != nil {
			return err
		}
		field.Set(elem)
		return nil
	}
	return convertAndSet(field, s)
}

var durationType = reflect.TypeOf(time.Duration(0))

// convertAndSet converts a scalar val to field's type and sets it.
func convertAndSet(field reflect.Value, val interface{}) error {
	if rv := reflect.ValueOf(val); rv.IsValid() && rv.Type() == field.Type() {
		field.Set(rv)
		return nil
	}

	// time.Duration: only strings. A bare number is ambiguous (30 would be 30ns).
	if field.Type() == durationType {
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("duration must be a string with a unit (e.g. \"30s\", \"1m30s\"), got %v", val)
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("cannot parse %q as duration (e.g. \"30s\", \"1m30s\")", s)
		}
		field.SetInt(int64(d))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		switch v := val.(type) {
		case string:
			field.SetString(v)
		case bool, int, int64, uint64, float64:
			field.SetString(fmt.Sprintf("%v", v)) // e.g. port: 8080 into a string field
		default:
			return fmt.Errorf("cannot convert %T to string", val)
		}

	case reflect.Bool:
		switch v := val.(type) {
		case bool:
			field.SetBool(v)
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("cannot parse %q as bool", v)
			}
			field.SetBool(b)
		default:
			return fmt.Errorf("cannot convert %T to bool", val)
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		switch v := val.(type) {
		case int:
			n = int64(v)
		case int64:
			n = v
		case uint64:
			if v > 1<<63-1 {
				return fmt.Errorf("value %d overflows %s", v, field.Type())
			}
			n = int64(v)
		case float64: // JSON numbers are float64
			if v != float64(int64(v)) {
				return fmt.Errorf("cannot convert %v to an integer without loss", v)
			}
			n = int64(v)
		case string:
			p, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as integer", v)
			}
			n = p
		default:
			return fmt.Errorf("cannot convert %T to integer", val)
		}
		if field.OverflowInt(n) {
			return fmt.Errorf("value %d overflows %s", n, field.Type())
		}
		field.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var n uint64
		switch v := val.(type) {
		case int:
			if v < 0 {
				return fmt.Errorf("negative value %d for %s", v, field.Type())
			}
			n = uint64(v)
		case int64:
			if v < 0 {
				return fmt.Errorf("negative value %d for %s", v, field.Type())
			}
			n = uint64(v)
		case uint64:
			n = v
		case float64:
			if v < 0 || v != float64(uint64(v)) {
				return fmt.Errorf("cannot convert %v to an unsigned integer without loss", v)
			}
			n = uint64(v)
		case string:
			p, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as unsigned integer", v)
			}
			n = p
		default:
			return fmt.Errorf("cannot convert %T to unsigned integer", val)
		}
		if field.OverflowUint(n) {
			return fmt.Errorf("value %d overflows %s", n, field.Type())
		}
		field.SetUint(n)

	case reflect.Float32, reflect.Float64:
		var f float64
		switch v := val.(type) {
		case float64:
			f = v
		case int:
			f = float64(v)
		case int64:
			f = float64(v)
		case string:
			p, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as float", v)
			}
			f = p
		default:
			return fmt.Errorf("cannot convert %T to float", val)
		}
		if field.OverflowFloat(f) {
			return fmt.Errorf("value %v overflows %s", f, field.Type())
		}
		field.SetFloat(f)

	case reflect.Interface:
		field.Set(reflect.ValueOf(val))

	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}
	return nil
}
