package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// canonicalJSON implements RFC 8785's UTF-16 property ordering, unescaped Unicode,
// and ECMAScript finite-number formatting for approval and idempotency digests.
func canonicalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	var enc func(any) error
	quote := func(s string) {
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 0x20 {
					fmt.Fprintf(&b, "\\u%04x", r)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
	}
	enc = func(v any) error {
		switch x := v.(type) {
		case nil:
			b.WriteString("null")
		case bool:
			if x {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}
		case string:
			quote(x)
		case json.Number:
			f, e := x.Float64()
			if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return fmt.Errorf("invalid JSON number")
			}
			if f == 0 {
				b.WriteByte('0')
				break
			}
			abs := math.Abs(f)
			s := strconv.FormatFloat(f, 'f', -1, 64)
			if abs >= 1e21 || abs < 1e-6 {
				s = strconv.FormatFloat(f, 'e', -1, 64)
				s = strings.ReplaceAll(s, "e-0", "e-")
				s = strings.ReplaceAll(s, "e+0", "e+")
			}
			b.WriteString(s)
		case []any:
			b.WriteByte('[')
			for i, v := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				if e := enc(v); e != nil {
					return e
				}
			}
			b.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				a, c := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
				for n := 0; n < len(a) && n < len(c); n++ {
					if a[n] != c[n] {
						return a[n] < c[n]
					}
				}
				return len(a) < len(c)
			})
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				quote(k)
				b.WriteByte(':')
				if e := enc(x[k]); e != nil {
					return e
				}
			}
			b.WriteByte('}')
		default:
			return fmt.Errorf("uncanonicalizable %T", v)
		}
		return nil
	}
	if e := enc(v); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
