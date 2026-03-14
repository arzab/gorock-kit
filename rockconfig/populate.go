package rockconfig

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"
)

// populate fills the struct v from the data map, respecting config tags.
func populate(data map[string]interface{}, v reflect.Value) error {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		structField := t.Field(i)
		fieldVal := v.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		ft := parseFieldTag(structField.Tag.Get("config"), structField.Name)
		if ft.ignore {
			continue
		}

		// Env var has highest priority.
		if ft.envVar != "" {
			if envVal, set := os.LookupEnv(ft.envVar); set {
				if err := setFromString(fieldVal, envVal); err != nil {
					return fmt.Errorf("field %q from env %s: %w", structField.Name, ft.envVar, err)
				}
				continue
			}
		}

		// Look up value in the data map.
		val, found := data[ft.name]
		if !found || val == nil {
			// Apply default if provided.
			if ft.defaultVal != "" {
				if err := setFromString(fieldVal, ft.defaultVal); err != nil {
					return fmt.Errorf("field %q default: %w", structField.Name, err)
				}
			}
			continue
		}

		if err := setValue(fieldVal, val); err != nil {
			return fmt.Errorf("field %q: %w", structField.Name, err)
		}
	}

	return nil
}

// setValue sets field to val, handling nested structs, pointers, slices and maps.
func setValue(field reflect.Value, val interface{}) error {
	// Dereference or allocate pointer.
	if field.Kind() == reflect.Ptr {
		if val == nil {
			return nil
		}
		elem := reflect.New(field.Type().Elem())
		if err := setValue(elem.Elem(), val); err != nil {
			return err
		}
		field.Set(elem)
		return nil
	}

	// Nested struct.
	if field.Kind() == reflect.Struct {
		nested, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object for struct field, got %T", val)
		}
		return populate(nested, field)
	}

	// Direct assignment if types match.
	rv := reflect.ValueOf(val)
	if rv.IsValid() && rv.Type().AssignableTo(field.Type()) {
		field.Set(rv)
		return nil
	}

	return convertAndSet(field, val)
}

var durationType = reflect.TypeOf(time.Duration(0))

// convertAndSet converts val to field's type and sets it.
func convertAndSet(field reflect.Value, val interface{}) error {
	// time.Duration: support both numeric (nanoseconds) and string ("5s", "1m30s").
	if field.Type() == durationType {
		switch v := val.(type) {
		case string:
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("cannot parse %q as duration (e.g. \"5s\", \"1m30s\")", v)
			}
			field.SetInt(int64(d))
			return nil
		case int:
			field.SetInt(int64(v))
			return nil
		case int64:
			field.SetInt(v)
			return nil
		case float64:
			field.SetInt(int64(v))
			return nil
		}
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", val))

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
		switch v := val.(type) {
		case int:
			field.SetInt(int64(v))
		case int64:
			field.SetInt(v)
		case float64: // JSON numbers are float64
			if v != float64(int64(v)) {
				return fmt.Errorf("cannot convert float %v to int without loss", v)
			}
			field.SetInt(int64(v))
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as int", v)
			}
			field.SetInt(n)
		default:
			return fmt.Errorf("cannot convert %T to int", val)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch v := val.(type) {
		case int:
			field.SetUint(uint64(v))
		case uint64:
			field.SetUint(v)
		case float64:
			if v < 0 || v != float64(uint64(v)) {
				return fmt.Errorf("cannot convert float %v to uint without loss", v)
			}
			field.SetUint(uint64(v))
		case string:
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as uint", v)
			}
			field.SetUint(n)
		default:
			return fmt.Errorf("cannot convert %T to uint", val)
		}

	case reflect.Float32, reflect.Float64:
		switch v := val.(type) {
		case float64:
			field.SetFloat(v)
		case float32:
			field.SetFloat(float64(v))
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as float", v)
			}
			field.SetFloat(f)
		default:
			return fmt.Errorf("cannot convert %T to float", val)
		}

	case reflect.Slice:
		return setSlice(field, val)

	case reflect.Map:
		return setMap(field, val)

	case reflect.Interface:
		field.Set(reflect.ValueOf(val))

	default:
		return fmt.Errorf("unsupported field type %s", field.Kind())
	}

	return nil
}

func setSlice(field reflect.Value, val interface{}) error {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("expected array, got %T", val)
	}

	slice := reflect.MakeSlice(field.Type(), rv.Len(), rv.Len())
	for i := 0; i < rv.Len(); i++ {
		if err := setValue(slice.Index(i), rv.Index(i).Interface()); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
	}
	field.Set(slice)
	return nil
}

func setMap(field reflect.Value, val interface{}) error {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Map {
		return fmt.Errorf("expected object, got %T", val)
	}

	m := reflect.MakeMap(field.Type())
	keyType := field.Type().Key()
	valType := field.Type().Elem()

	for _, key := range rv.MapKeys() {
		mapKey := reflect.New(keyType).Elem()
		if err := setValue(mapKey, key.Interface()); err != nil {
			return fmt.Errorf("key %v: %w", key, err)
		}

		mapVal := reflect.New(valType).Elem()
		if err := setValue(mapVal, rv.MapIndex(key).Interface()); err != nil {
			return fmt.Errorf("[%v]: %w", key, err)
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
