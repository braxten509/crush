package styles

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

// dumpValue writes every leaf of v as "path = value" lines. It walks
// unexported fields too and never prints type names, so the dump stays the
// same when a struct type is only renamed or moved.
func dumpValue(b *strings.Builder, path string, v reflect.Value) {
	switch v.Kind() {
	case reflect.Invalid:
		fmt.Fprintf(b, "%s = <invalid>\n", path)
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			fmt.Fprintf(b, "%s = nil\n", path)
			return
		}
		dumpValue(b, path, v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			dumpValue(b, path+"."+v.Type().Field(i).Name, v.Field(i))
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			fmt.Fprintf(b, "%s = nil\n", path)
			return
		}
		for i := range v.Len() {
			dumpValue(b, fmt.Sprintf("%s[%d]", path, i), v.Index(i))
		}
	case reflect.Map:
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
		})
		for _, k := range keys {
			dumpValue(b, fmt.Sprintf("%s[%v]", path, k), v.MapIndex(k))
		}
	case reflect.Func:
		fmt.Fprintf(b, "%s = func(nil=%v)\n", path, v.IsNil())
	case reflect.Bool:
		fmt.Fprintf(b, "%s = %v\n", path, v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(b, "%s = %d\n", path, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(b, "%s = %d\n", path, v.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(b, "%s = %v\n", path, v.Float())
	case reflect.String:
		fmt.Fprintf(b, "%s = %q\n", path, v.String())
	default:
		fmt.Fprintf(b, "%s = <%s>\n", path, v.Kind())
	}
}

// dumpStyles returns the full dump of s.
func dumpStyles(s Styles) string {
	var b strings.Builder
	dumpValue(&b, "Styles", reflect.ValueOf(s))
	return b.String()
}

// TestThemeStylesDump pins the complete Styles value of every built-in
// theme, so reorganizing how styles are declared or built cannot change a
// single style. Each theme is summarized by a hash of its full dump.
func TestThemeStylesDump(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for _, name := range BuiltinThemeNames() {
		s := quickStyle(builtinThemes[name]())
		if override, ok := builtinThemeOverrides[name]; ok {
			s = override(s)
		}
		dump := dumpStyles(s)
		fmt.Fprintf(&b, "%s: leaves=%d sha256=%x\n", name, strings.Count(dump, "\n"), sha256.Sum256([]byte(dump)))
	}
	golden.RequireEqual(t, []byte(b.String()))
}
