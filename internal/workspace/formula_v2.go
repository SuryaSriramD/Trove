package workspace

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Version 2 is opt-in. Version 1 source/import semantics remain unchanged.
// Values are inert: lists contain homogeneous scalars or typed references/dates.
const formulaListLimit = 256

var formulaV2Functions = map[string]string{
	"list":  "list(value, …): up to 256 homogeneous values; nulls permitted",
	"first": "first(list): first item or null", "last": "last(list): last item or null",
	"at":     "at(list, index): zero-based index; outside list returns null",
	"slice":  "slice(list, start, end): nonnegative half-open indexes, clipped at length",
	"join":   "join(list, separator): format scalar items as text",
	"unique": "unique(list): preserve first occurrence", "sort": "sort(list): ascending numbers/text/dates",
	"reverse": "reverse(list): reverse order", "format": "format(value): explicit scalar/date/reference conversion",
	"toNumber":     "toNumber(text): strict decimal conversion; no locale separators",
	"regexMatch":   "regexMatch(text, pattern): bounded RE2 search",
	"regexReplace": "regexReplace(text, pattern, replacement): literal replacement, at most 1024 matches",
	"date":         "date(ISO text): date-only or RFC3339 timestamp, in explicit evaluation timezone",
	"dateStart":    "dateStart(date): start as a date", "dateEnd": "dateEnd(date): end, or start when absent",
	"dateAdd":     "dateAdd(date, integer, unit): minutes/hours elapsed; days/weeks/months/years calendar, month-end clamped",
	"dateBetween": "dateBetween(end, start, unit): fractional elapsed minutes/hours; civil days/weeks in evaluation zone",
	"formatDate":  "formatDate(date, pattern): YYYY MM DD HH mm ss Z tokens only",
	"name":        "name(page/person reference): display label", "id": "id(page/person reference): stable or source ID",
}

func formulaV2List(v any) ([]any, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list")
	}
	if len(xs) > formulaListLimit {
		return nil, fmt.Errorf("list exceeds 256 values")
	}
	kind := ""
	for _, x := range xs {
		if x == nil {
			continue
		}
		k := formulaV2Type(x)
		if k == "number" {
			if _, err := formulaNumber(x); err != nil {
				return nil, err
			}
		}
		if k == "list" || k == "unsupported" {
			return nil, fmt.Errorf("lists require supported scalar values")
		}
		if kind != "" && kind != k {
			return nil, fmt.Errorf("list values must have one type")
		}
		kind = k
	}
	return xs, nil
}
func formulaV2Type(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "text"
	case bool:
		return "boolean"
	case *big.Rat:
		return "number"
	case []any:
		return "list"
	case map[string]any:
		if _, ok := x["decimal"]; ok {
			return "number"
		}
		if _, ok := x["start"]; ok {
			return "date"
		}
		if x["kind"] == "page" || x["kind"] == "person" {
			return str(x["kind"])
		}
	}
	return "unsupported"
}
func formulaV2Integer(v any) (int, error) {
	n, e := formulaNumber(v)
	if e != nil || !n.IsInt() || !n.Num().IsInt64() {
		return 0, fmt.Errorf("expected a bounded integer")
	}
	i := n.Num().Int64()
	if i < -1000000 || i > 1000000 {
		return 0, fmt.Errorf("integer exceeds supported range")
	}
	return int(i), nil
}
func formulaV2Format(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case *big.Rat:
		return decimalRat(x), nil
	case map[string]any:
		switch formulaV2Type(v) {
		case "number":
			n, e := formulaNumber(v)
			if e != nil {
				return "", e
			}
			return decimalRat(n), nil
		case "date":
			return str(x["start"]), nil
		case "page", "person":
			return str(x["name"]), nil
		}
	}
	return "", fmt.Errorf("format requires a supported scalar; use join for lists")
}
func formulaV2Equal(a, b any) bool {
	if x, e := formulaNumber(a); e == nil {
		if y, e := formulaNumber(b); e == nil {
			return x.Cmp(y) == 0
		}
	}
	return string(marshal(formulaPublicValue(a))) == string(marshal(formulaPublicValue(b)))
}
func (f *formulaInterpreter) callV2(name string, a []any) (any, error) {
	arity := func(n int) error {
		if len(a) != n {
			return fmt.Errorf("%s requires %d arguments", name, n)
		}
		return nil
	}
	if name == "list" {
		return formulaV2List(a)
	}
	if name == "length" || name == "contains" {
		if len(a) > 0 {
			if _, ok := a[0].([]any); ok {
				want := 1
				if name == "contains" {
					want = 2
				}
				if e := arity(want); e != nil {
					return nil, e
				}
				xs, e := formulaV2List(a[0])
				if e != nil {
					return nil, e
				}
				f.work += len(xs)
				if name == "length" {
					return new(big.Rat).SetInt64(int64(len(xs))), nil
				}
				for _, x := range xs {
					if formulaV2Equal(x, a[1]) {
						return true, nil
					}
				}
				return false, nil
			}
		}
		return f.callV1(name, a)
	}
	switch name {
	case "first", "last", "at", "slice", "join", "unique", "sort", "reverse":
		n := 1
		if name == "at" || name == "join" {
			n = 2
		}
		if name == "slice" {
			n = 3
		}
		if e := arity(n); e != nil {
			return nil, e
		}
		xs, e := formulaV2List(a[0])
		if e != nil {
			return nil, e
		}
		f.work += len(xs)
		xs = append([]any{}, xs...)
		switch name {
		case "first", "last":
			if len(xs) == 0 {
				return nil, nil
			}
			if name == "last" {
				return xs[len(xs)-1], nil
			}
			return xs[0], nil
		case "at":
			i, e := formulaV2Integer(a[1])
			if e != nil {
				return nil, e
			}
			if i < 0 || i >= len(xs) {
				return nil, nil
			}
			return xs[i], nil
		case "slice":
			start, e := formulaV2Integer(a[1])
			if e != nil {
				return nil, e
			}
			end, e := formulaV2Integer(a[2])
			if e != nil {
				return nil, e
			}
			if start < 0 || end < start {
				return nil, fmt.Errorf("slice requires 0 <= start <= end")
			}
			start = min(start, len(xs))
			end = min(end, len(xs))
			return xs[start:end], nil
		case "reverse":
			for i, j := 0, len(xs)-1; i < j; i, j = i+1, j-1 {
				xs[i], xs[j] = xs[j], xs[i]
			}
			return xs, nil
		case "unique":
			out := []any{}
			for _, x := range xs {
				seen := false
				for _, y := range out {
					f.work++
					if f.work > 1024 {
						return nil, fmt.Errorf("formula work limit exceeded")
					}
					if formulaV2Equal(x, y) {
						seen = true
						break
					}
				}
				if !seen {
					out = append(out, x)
				}
			}
			return out, nil
		case "join":
			sep, ok := a[1].(string)
			if !ok {
				return nil, fmt.Errorf("join separator must be text")
			}
			var out strings.Builder
			for i, x := range xs {
				text, e := formulaV2Format(x)
				if e != nil {
					return nil, e
				}
				extra := len(text)
				if i > 0 {
					extra += len(sep)
				}
				if out.Len()+extra > 65536 {
					return nil, fmt.Errorf("result exceeds 65536 bytes")
				}
				if i > 0 {
					out.WriteString(sep)
				}
				out.WriteString(text)
			}
			return out.String(), nil
		case "sort":
			for _, value := range xs {
				if value == nil {
					continue
				}
				kind := formulaV2Type(value)
				if kind != "number" && kind != "text" && kind != "date" {
					return nil, fmt.Errorf("sort supports numbers, text and dates")
				}
				if kind == "date" {
					if _, _, err := formulaV2Date(value, f.clock.Timezone); err != nil {
						return nil, err
					}
				}
			}
			var sortErr error
			sort.SliceStable(xs, func(i, j int) bool {
				f.work++
				if f.work > 1024 {
					sortErr = fmt.Errorf("formula work limit exceeded")
					return false
				}
				if xs[i] == nil {
					return xs[j] != nil
				}
				if xs[j] == nil {
					return false
				}
				if n, e := formulaNumber(xs[i]); e == nil {
					m, _ := formulaNumber(xs[j])
					return n.Cmp(m) < 0
				}
				if x, ok := xs[i].(string); ok {
					return x < xs[j].(string)
				}
				if formulaV2Type(xs[i]) == "date" {
					x, _, e := formulaV2Date(xs[i], f.clock.Timezone)
					y, _, e2 := formulaV2Date(xs[j], f.clock.Timezone)
					if e != nil || e2 != nil {
						sortErr = fmt.Errorf("invalid date in list")
						return false
					}
					return x.Before(y)
				}
				sortErr = fmt.Errorf("sort supports numbers, text and dates")
				return false
			})
			return xs, sortErr
		}
	case "format":
		if e := arity(1); e != nil {
			return nil, e
		}
		return formulaV2Format(a[0])
	case "toNumber":
		if e := arity(1); e != nil {
			return nil, e
		}
		s, ok := a[0].(string)
		if !ok {
			return nil, fmt.Errorf("toNumber requires text")
		}
		return parseFormulaNumber(s)
	case "name", "id":
		if e := arity(1); e != nil {
			return nil, e
		}
		x, ok := a[0].(map[string]any)
		if !ok || (x["kind"] != "page" && x["kind"] != "person") {
			return nil, fmt.Errorf("%s requires a page or person reference", name)
		}
		return str(x[name]), nil
	case "regexMatch", "regexReplace":
		n := 2
		if name == "regexReplace" {
			n = 3
		}
		if e := arity(n); e != nil {
			return nil, e
		}
		s, ok := a[0].(string)
		pattern, ok2 := a[1].(string)
		if !ok || !ok2 {
			return nil, fmt.Errorf("regex requires text arguments")
		}
		if len(s) > 65536 || len(pattern) > 1024 {
			return nil, fmt.Errorf("regex input/pattern limit exceeded")
		}
		re, e := regexp.Compile(pattern)
		if e != nil {
			return nil, fmt.Errorf("invalid RE2 pattern: %s", e)
		}
		if name == "regexMatch" {
			return re.MatchString(s), nil
		}
		replacement, ok := a[2].(string)
		if !ok {
			return nil, fmt.Errorf("replacement must be text")
		}
		matches := re.FindAllStringIndex(s, 1025)
		if len(matches) > 1024 {
			return nil, fmt.Errorf("regex exceeds 1024 matches")
		}
		var out strings.Builder
		end := 0
		for _, match := range matches {
			if out.Len()+match[0]-end+len(replacement) > 65536 {
				return nil, fmt.Errorf("result exceeds 65536 bytes")
			}
			out.WriteString(s[end:match[0]])
			out.WriteString(replacement)
			end = match[1]
		}
		if out.Len()+len(s)-end > 65536 {
			return nil, fmt.Errorf("result exceeds 65536 bytes")
		}
		out.WriteString(s[end:])
		return out.String(), nil
	case "date":
		if e := arity(1); e != nil {
			return nil, e
		}
		s, ok := a[0].(string)
		if !ok {
			return nil, fmt.Errorf("date requires ISO text")
		}
		loc, _ := time.LoadLocation(f.clock.Timezone)
		timed := strings.Contains(s, "T")
		var at time.Time
		var e error
		if timed {
			at, e = time.Parse(time.RFC3339, s)
		} else {
			at, e = time.ParseInLocation("2006-01-02", s, loc)
		}
		if e != nil || at.Year() < 1 || at.Year() > 9999 {
			return nil, fmt.Errorf("invalid ISO date")
		}
		return formulaV2DateValue(at.In(loc), timed, f.clock.Timezone), nil
	case "dateStart", "dateEnd", "dateAdd", "dateBetween", "formatDate":
		return f.callV2Date(name, a)
	}
	return f.callV1(name, a)
}
func formulaV2Date(v any, zone string) (time.Time, bool, error) {
	x, ok := v.(map[string]any)
	if !ok {
		return time.Time{}, false, fmt.Errorf("expected a date")
	}
	if z := str(x["timezone"]); z != "" {
		zone = z
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, false, fmt.Errorf("unknown date timezone")
	}
	timed, _ := x["hasTime"].(bool)
	s := str(x["start"])
	var at time.Time
	if timed {
		at, e = time.Parse(time.RFC3339, s)
	} else {
		at, e = time.ParseInLocation("2006-01-02", s, loc)
	}
	if e != nil || at.Year() < 1 || at.Year() > 9999 {
		return time.Time{}, false, fmt.Errorf("invalid date value")
	}
	return at.In(loc), timed, nil
}
func formulaV2DateValue(at time.Time, timed bool, zone string) map[string]any {
	s := at.Format("2006-01-02")
	if timed {
		s = at.Format(time.RFC3339Nano)
	}
	return map[string]any{"start": s, "end": nil, "hasTime": timed, "timezone": zone}
}
func (f *formulaInterpreter) callV2Date(name string, a []any) (any, error) {
	n := 1
	if name == "formatDate" {
		n = 2
	}
	if name == "dateAdd" || name == "dateBetween" {
		n = 3
	}
	if len(a) != n {
		return nil, fmt.Errorf("%s requires %d arguments", name, n)
	}
	at, timed, e := formulaV2Date(a[0], f.clock.Timezone)
	if e != nil {
		return nil, e
	}
	zone := str(a[0].(map[string]any)["timezone"])
	if zone == "" {
		zone = f.clock.Timezone
	}
	switch name {
	case "dateStart":
		return formulaV2DateValue(at, timed, zone), nil
	case "dateEnd":
		x := a[0].(map[string]any)
		if end := str(x["end"]); end != "" {
			copy := map[string]any{"start": end, "hasTime": timed, "timezone": zone}
			at, timed, e = formulaV2Date(copy, zone)
			if e != nil {
				return nil, e
			}
		}
		return formulaV2DateValue(at, timed, zone), nil
	case "dateAdd":
		amount, e := formulaV2Integer(a[1])
		if e != nil {
			return nil, e
		}
		unit, ok := a[2].(string)
		if !ok {
			return nil, fmt.Errorf("date unit must be text")
		}
		target := at
		switch unit {
		case "minutes", "hours":
			if !timed {
				return nil, fmt.Errorf("elapsed units require a timed date")
			}
			mult := time.Minute
			if unit == "hours" {
				mult = time.Hour
			}
			target = at.Add(time.Duration(amount) * mult)
		case "days", "weeks", "months", "years":
			y, m, d := at.Date()
			if unit == "weeks" {
				amount *= 7
				unit = "days"
			}
			if unit == "years" {
				amount *= 12
				unit = "months"
			}
			if unit == "days" {
				civil := time.Date(y, m, d+amount, 12, 0, 0, 0, time.UTC)
				y, m, d = civil.Date()
			} else {
				civil := time.Date(y, m+time.Month(amount), 1, 12, 0, 0, 0, time.UTC)
				y, m, _ = civil.Date()
				last := time.Date(y, m+1, 0, 12, 0, 0, 0, time.UTC).Day()
				d = min(d, last)
			}
			target, e = formulaCalendarTime(y, m, d, at)
			if e != nil {
				return nil, e
			}
		default:
			return nil, fmt.Errorf("supported dateAdd units: minutes, hours, days, weeks, months, years")
		}
		if target.Year() < 1 || target.Year() > 9999 {
			return nil, fmt.Errorf("date outside years 0001–9999")
		}
		return formulaV2DateValue(target, timed, zone), nil
	case "dateBetween":
		start, _, e := formulaV2Date(a[1], f.clock.Timezone)
		if e != nil {
			return nil, e
		}
		unit, ok := a[2].(string)
		if !ok {
			return nil, fmt.Errorf("date unit must be text")
		}
		var numerator int64
		var denominator int64 = 1
		switch unit {
		case "minutes", "hours":
			numerator = at.Unix() - start.Unix()
			denominator = 60
			if unit == "hours" {
				denominator = 3600
			}
			ns := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(1000000000))
			ns.Add(ns, big.NewInt(int64(at.Nanosecond()-start.Nanosecond())))
			return new(big.Rat).SetFrac(ns, big.NewInt(denominator*1000000000)), nil
		case "days", "weeks":
			loc, _ := time.LoadLocation(f.clock.Timezone)
			a, b := at.In(loc), start.In(loc)
			civilA := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
			civilB := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
			numerator = (civilA.Unix() - civilB.Unix()) / 86400
			if unit == "weeks" {
				denominator = 7
			}
		default:
			return nil, fmt.Errorf("supported dateBetween units: minutes, hours, days, weeks")
		}
		return new(big.Rat).SetFrac(big.NewInt(numerator), big.NewInt(denominator)), nil
	case "formatDate":
		pattern, ok := a[1].(string)
		if !ok || len(pattern) > 256 {
			return nil, fmt.Errorf("date format must be text up to 256 bytes")
		}
		for _, unsupported := range []string{"MMM", "DDD", "YYYYY", "HHH", "mmm", "sss", "ZZ"} {
			if strings.Contains(pattern, unsupported) {
				return nil, fmt.Errorf("unsupported date format token")
			}
		}
		tokens := map[string]string{"YYYY": "2006", "MM": "01", "DD": "02", "HH": "15", "mm": "04", "ss": "05", "Z": "-07:00"}
		var out strings.Builder
		for len(pattern) > 0 {
			matched := false
			for _, token := range []string{"YYYY", "MM", "DD", "HH", "mm", "ss", "Z"} {
				if strings.HasPrefix(pattern, token) {
					out.WriteString(at.Format(tokens[token]))
					pattern = pattern[len(token):]
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			r, size := utf8.DecodeRuneInString(pattern)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				return nil, fmt.Errorf("unsupported date format token")
			}
			out.WriteRune(r)
			pattern = pattern[size:]
		}
		return out.String(), nil
	}
	return nil, fmt.Errorf("unsupported date operation")
}

// Calendar ambiguity is deterministic: use the earlier instant when a wall time
// occurs twice, and reject gaps instead of silently shifting the requested time.
func formulaCalendarTime(year int, month time.Month, day int, source time.Time) (time.Time, error) {
	civil := time.Date(year, month, day, source.Hour(), source.Minute(), source.Second(), source.Nanosecond(), time.UTC)
	var chosen time.Time
	offsets := map[int]bool{}
	for _, hours := range []int{-36, 0, 36} {
		_, offset := civil.Add(time.Duration(hours) * time.Hour).In(source.Location()).Zone()
		offsets[offset] = true
	}
	for offset := range offsets {
		candidate := civil.Add(-time.Duration(offset) * time.Second).In(source.Location())
		if candidate.Year() == year && candidate.Month() == month && candidate.Day() == day && candidate.Hour() == source.Hour() && candidate.Minute() == source.Minute() && candidate.Second() == source.Second() && (chosen.IsZero() || candidate.Before(chosen)) {
			chosen = candidate
		}
	}
	if chosen.IsZero() {
		return time.Time{}, fmt.Errorf("date lands in a nonexistent local time")
	}
	return chosen, nil
}
