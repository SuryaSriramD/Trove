package workspace

// The formula interpreter accepts a deliberately small expression language. It
// never evaluates Go code: go/parser supplies an AST and this file walks only
// explicitly permitted node types, functions and bounded property references.
import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type FormulaClock struct {
	At       time.Time
	Timezone string
}
type formulaInterpreter struct {
	clock  FormulaClock
	lookup func(string) (any, error)
	work   int
}

func EvaluateFormula(expression string, clock FormulaClock, lookup func(string) (any, error)) (any, error) {
	if len(expression) > 8192 {
		return nil, fmt.Errorf("expression exceeds 8192 bytes")
	}
	if _, err := time.LoadLocation(clock.Timezone); err != nil {
		return nil, fmt.Errorf("unknown timezone")
	}
	if clock.At.IsZero() {
		return nil, fmt.Errorf("explicit clock required")
	}
	tree, err := parser.ParseExpr(expression)
	if err != nil {
		return nil, fmt.Errorf("unsupported expression syntax: %s", err)
	}
	if _, err = formulaTreeDependencies(tree); err != nil {
		return nil, err
	}
	f := formulaInterpreter{clock: clock, lookup: lookup}
	v, err := f.eval(tree, 0)
	if err != nil {
		return nil, err
	}
	return formulaPublicValue(v), nil
}
func formulaPublicValue(v any) any {
	if n, ok := v.(*big.Rat); ok {
		return map[string]any{"decimal": decimalRat(n), "format": "decimal"}
	}
	return v
}
func decimalRat(n *big.Rat) string {
	s := n.FloatString(12)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return s
}
func formulaNumber(v any) (*big.Rat, error) {
	switch x := v.(type) {
	case *big.Rat:
		return boundedFormulaNumber(x)
	case map[string]any:
		if s, ok := x["decimal"].(string); ok {
			return parseFormulaNumber(s)
		}
	case float64:
		return boundedFormulaNumber(new(big.Rat).SetFloat64(x))
	}
	return nil, fmt.Errorf("expected a number")
}

var formulaDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE]([+-]?[0-9]+))?$`)

func parseFormulaNumber(s string) (*big.Rat, error) {
	// Bound input before big.Rat expands an exponent. A short hostile literal
	// such as 1e999999999 otherwise allocates far beyond the evaluation budget.
	if len(s) > 8192 {
		return nil, fmt.Errorf("numeric work limit exceeded")
	}
	parts := formulaDecimal.FindStringSubmatch(s)
	if parts == nil {
		return nil, fmt.Errorf("expected a decimal number")
	}
	if parts[1] != "" {
		exponent, err := strconv.ParseInt(parts[1], 10, 32)
		if err != nil || exponent > 4096 || exponent < -4096 {
			return nil, fmt.Errorf("numeric work limit exceeded")
		}
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("expected a number")
	}
	return boundedFormulaNumber(n)
}

func boundedFormulaNumber(n *big.Rat) (*big.Rat, error) {
	if n == nil {
		return nil, fmt.Errorf("expected a finite number")
	}
	if n.Num().BitLen() > 4096 || n.Denom().BitLen() > 4096 {
		return nil, fmt.Errorf("numeric work limit exceeded")
	}
	return n, nil
}
func formulaTruth(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expected a boolean")
	}
	return b, nil
}
func (f *formulaInterpreter) eval(node ast.Expr, depth int) (any, error) {
	f.work++
	if depth > 48 || f.work > 1024 {
		return nil, fmt.Errorf("formula work limit exceeded")
	}
	switch n := node.(type) {
	case *ast.ParenExpr:
		return f.eval(n.X, depth+1)
	case *ast.BasicLit:
		if n.Kind == token.STRING {
			s, e := strconv.Unquote(n.Value)
			return s, e
		}
		if n.Kind == token.INT || n.Kind == token.FLOAT {
			if len(n.Value) < 128 {
				return parseFormulaNumber(n.Value)
			}
		}
		return nil, fmt.Errorf("unsupported literal")
	case *ast.Ident:
		switch n.Name {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return nil, fmt.Errorf("unknown identifier %q", n.Name)
	case *ast.UnaryExpr:
		v, e := f.eval(n.X, depth+1)
		if e != nil {
			return nil, e
		}
		if n.Op == token.NOT {
			b, e := formulaTruth(v)
			return !b, e
		}
		num, e := formulaNumber(v)
		if e != nil {
			return nil, e
		}
		if n.Op == token.SUB {
			return new(big.Rat).Neg(num), nil
		}
		if n.Op == token.ADD {
			return num, nil
		}
		return nil, fmt.Errorf("unsupported unary operator")
	case *ast.BinaryExpr:
		left, e := f.eval(n.X, depth+1)
		if e != nil {
			return nil, e
		}
		if n.Op == token.LAND || n.Op == token.LOR {
			b, e := formulaTruth(left)
			if e != nil {
				return nil, e
			}
			if n.Op == token.LAND && !b || n.Op == token.LOR && b {
				return b, nil
			}
			right, e := f.eval(n.Y, depth+1)
			if e != nil {
				return nil, e
			}
			return formulaTruth(right)
		}
		right, e := f.eval(n.Y, depth+1)
		if e != nil {
			return nil, e
		}
		if n.Op == token.EQL || n.Op == token.NEQ {
			eq := string(marshal(formulaPublicValue(left))) == string(marshal(formulaPublicValue(right)))
			if a, ae := formulaNumber(left); ae == nil {
				if b, be := formulaNumber(right); be == nil {
					eq = a.Cmp(b) == 0
				}
			}
			if n.Op == token.NEQ {
				eq = !eq
			}
			return eq, nil
		}
		if a, ok := left.(string); ok {
			b, ok := right.(string)
			if !ok {
				return nil, fmt.Errorf("text operands must both be text")
			}
			switch n.Op {
			case token.ADD:
				if len(a)+len(b) > 65536 {
					return nil, fmt.Errorf("result exceeds 65536 bytes")
				}
				return a + b, nil
			case token.LSS:
				return a < b, nil
			case token.GTR:
				return a > b, nil
			case token.LEQ:
				return a <= b, nil
			case token.GEQ:
				return a >= b, nil
			}
		}
		a, e := formulaNumber(left)
		if e != nil {
			return nil, e
		}
		b, e := formulaNumber(right)
		if e != nil {
			return nil, e
		}
		var out *big.Rat
		switch n.Op {
		case token.ADD:
			out = new(big.Rat).Add(a, b)
		case token.SUB:
			out = new(big.Rat).Sub(a, b)
		case token.MUL:
			out = new(big.Rat).Mul(a, b)
		case token.QUO:
			if b.Sign() == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			out = new(big.Rat).Quo(a, b)
		case token.LSS:
			return a.Cmp(b) < 0, nil
		case token.GTR:
			return a.Cmp(b) > 0, nil
		case token.LEQ:
			return a.Cmp(b) <= 0, nil
		case token.GEQ:
			return a.Cmp(b) >= 0, nil
		default:
			return nil, fmt.Errorf("unsupported operator %s", n.Op)
		}
		if out.Num().BitLen() > 4096 || out.Denom().BitLen() > 4096 {
			return nil, fmt.Errorf("numeric work limit exceeded")
		}
		return out, nil
	case *ast.CallExpr:
		fn, ok := n.Fun.(*ast.Ident)
		if !ok || n.Ellipsis.IsValid() {
			return nil, fmt.Errorf("only named formula functions are supported")
		}
		if fn.Name == "ifElse" {
			if len(n.Args) != 3 {
				return nil, fmt.Errorf("ifElse requires three arguments")
			}
			v, e := f.eval(n.Args[0], depth+1)
			if e != nil {
				return nil, e
			}
			b, e := formulaTruth(v)
			if e != nil {
				return nil, e
			}
			if b {
				return f.eval(n.Args[1], depth+1)
			}
			return f.eval(n.Args[2], depth+1)
		}
		args := []any{}
		for _, a := range n.Args {
			v, e := f.eval(a, depth+1)
			if e != nil {
				return nil, e
			}
			args = append(args, v)
		}
		return f.call(fn.Name, args)
	}
	return nil, fmt.Errorf("unsupported expression node %T", node)
}
func (f *formulaInterpreter) call(name string, a []any) (any, error) {
	arity := func(n int) error {
		if len(a) != n {
			return fmt.Errorf("%s requires %d arguments", name, n)
		}
		return nil
	}
	switch name {
	case "prop":
		if e := arity(1); e != nil {
			return nil, e
		}
		id, ok := a[0].(string)
		if !ok {
			return nil, fmt.Errorf("prop requires a stable property ID")
		}
		if f.lookup == nil {
			return nil, fmt.Errorf("property lookup is unavailable")
		}
		v, err := f.lookup(id)
		if err != nil {
			return nil, err
		}
		if number, ok := v.(map[string]any); ok {
			if _, numeric := number["decimal"]; numeric {
				return formulaNumber(number)
			}
		}
		return v, nil
	case "now", "today":
		if e := arity(0); e != nil {
			return nil, e
		}
		loc, _ := time.LoadLocation(f.clock.Timezone)
		at := f.clock.At.In(loc)
		if name == "today" {
			return at.Format("2006-01-02"), nil
		}
		return at.Format(time.RFC3339), nil
	case "empty":
		if e := arity(1); e != nil {
			return nil, e
		}
		switch x := a[0].(type) {
		case nil:
			return true, nil
		case string:
			return x == "", nil
		case []any:
			return len(x) == 0, nil
		}
		return false, nil
	case "length", "lower", "upper":
		if e := arity(1); e != nil {
			return nil, e
		}
		s, ok := a[0].(string)
		if !ok {
			return nil, fmt.Errorf("%s requires text", name)
		}
		if name == "length" {
			return new(big.Rat).SetInt64(int64(utf8.RuneCountInString(s))), nil
		}
		if name == "lower" {
			return strings.ToLower(s), nil
		}
		return strings.ToUpper(s), nil
	case "contains":
		if e := arity(2); e != nil {
			return nil, e
		}
		s, ok := a[0].(string)
		sub, ok2 := a[1].(string)
		if !ok || !ok2 {
			return nil, fmt.Errorf("contains requires text arguments")
		}
		return strings.Contains(s, sub), nil
	case "concat":
		if len(a) > 64 {
			return nil, fmt.Errorf("concat accepts at most 64 arguments")
		}
		s := ""
		for _, v := range a {
			t, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("concat requires text")
			}
			s += t
			if len(s) > 65536 {
				return nil, fmt.Errorf("result exceeds 65536 bytes")
			}
		}
		return s, nil
	case "abs", "round":
		if e := arity(1); e != nil {
			return nil, e
		}
		n, e := formulaNumber(a[0])
		if e != nil {
			return nil, e
		}
		if name == "abs" {
			return new(big.Rat).Abs(n), nil
		}
		s := n.FloatString(0)
		v, _ := new(big.Rat).SetString(s)
		return v, nil
	case "min", "max":
		if len(a) == 0 || len(a) > 64 {
			return nil, fmt.Errorf("%s accepts 1–64 numbers", name)
		}
		v, e := formulaNumber(a[0])
		if e != nil {
			return nil, e
		}
		for _, raw := range a[1:] {
			n, e := formulaNumber(raw)
			if e != nil {
				return nil, e
			}
			if name == "min" && n.Cmp(v) < 0 || name == "max" && n.Cmp(v) > 0 {
				v = n
			}
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported function %q; original expression and captured evidence are retained", name)
	}
}
func formulaDependencies(expression string) ([]string, error) {
	tree, err := parser.ParseExpr(expression)
	if err != nil {
		return nil, err
	}
	return formulaTreeDependencies(tree)
}
func formulaTreeDependencies(tree ast.Expr) ([]string, error) {
	seen := map[string]bool{}
	ids := []string{}
	var dependencyErr error
	ast.Inspect(tree, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "prop" {
			return true
		}
		if len(call.Args) != 1 {
			dependencyErr = fmt.Errorf("prop requires one stable property ID")
			return false
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			dependencyErr = fmt.Errorf("dynamic property references are unsupported")
			return false
		}
		id, e := strconv.Unquote(lit.Value)
		if e != nil {
			dependencyErr = e
			return false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		return true
	})
	return ids, dependencyErr
}
