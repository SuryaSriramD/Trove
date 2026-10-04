package workspace

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

var formulaV1Functions = map[string]string{"prop": "prop(stable property ID): scoped property value", "ifElse": "ifElse(boolean, yes, no): only selected branch evaluates", "now": "now(): explicit clock RFC3339 text", "today": "today(): explicit clock date text", "empty": "empty(value): null, empty text or empty list", "length": "length(text/list): Unicode code points or list items", "lower": "lower(text): Unicode lowercase", "upper": "upper(text): Unicode uppercase", "contains": "contains(text, text) or contains(list, scalar)", "concat": "concat(text, …): 0–64 text arguments", "abs": "abs(number)", "round": "round(number): integer, ties away from zero", "min": "min(number, …): 1–64 numbers", "max": "max(number, …): 1–64 numbers"}
var formulaArity = map[string][2]int{"prop": {1, 1}, "ifElse": {3, 3}, "now": {0, 0}, "today": {0, 0}, "empty": {1, 1}, "length": {1, 1}, "lower": {1, 1}, "upper": {1, 1}, "contains": {2, 2}, "concat": {0, 64}, "abs": {1, 1}, "round": {1, 1}, "min": {1, 64}, "max": {1, 64}, "list": {0, 256}, "first": {1, 1}, "last": {1, 1}, "at": {2, 2}, "slice": {3, 3}, "join": {2, 2}, "unique": {1, 1}, "sort": {1, 1}, "reverse": {1, 1}, "format": {1, 1}, "toNumber": {1, 1}, "regexMatch": {2, 2}, "regexReplace": {3, 3}, "date": {1, 1}, "dateStart": {1, 1}, "dateEnd": {1, 1}, "dateAdd": {3, 3}, "dateBetween": {3, 3}, "formatDate": {2, 2}, "name": {1, 1}, "id": {1, 1}}

func formulaCompileV2(expression string) (any, []string, error) {
	if len(expression) > 8192 {
		return nil, nil, fmt.Errorf("expression exceeds 8192 bytes")
	}
	tree, e := parser.ParseExpr(expression)
	if e != nil {
		return nil, nil, fmt.Errorf("unsupported expression syntax: %s", e)
	}
	deps, e := formulaTreeDependencies(tree)
	if e != nil {
		return nil, nil, e
	}
	work := 0
	var visit func(ast.Expr, int) (any, error)
	visit = func(expr ast.Expr, depth int) (any, error) {
		work++
		if depth > 48 || work > 1024 {
			return nil, fmt.Errorf("formula work limit exceeded")
		}
		switch n := expr.(type) {
		case *ast.ParenExpr:
			return visit(n.X, depth+1)
		case *ast.BasicLit:
			if n.Kind != token.STRING && n.Kind != token.INT && n.Kind != token.FLOAT {
				return nil, fmt.Errorf("unsupported literal")
			}
			if n.Kind == token.INT || n.Kind == token.FLOAT {
				if _, err := parseFormulaNumber(n.Value); err != nil {
					return nil, err
				}
			}
			return map[string]any{"kind": "literal", "token": n.Kind.String(), "value": n.Value}, nil
		case *ast.Ident:
			if n.Name != "true" && n.Name != "false" && n.Name != "null" {
				return nil, fmt.Errorf("unknown identifier %q", n.Name)
			}
			return map[string]any{"kind": "literal", "value": n.Name}, nil
		case *ast.UnaryExpr:
			if n.Op != token.ADD && n.Op != token.SUB && n.Op != token.NOT {
				return nil, fmt.Errorf("unsupported unary operator")
			}
			x, e := visit(n.X, depth+1)
			return map[string]any{"kind": "unary", "operator": n.Op.String(), "value": x}, e
		case *ast.BinaryExpr:
			switch n.Op {
			case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.LAND, token.LOR, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				return nil, fmt.Errorf("unsupported operator %s", n.Op)
			}
			x, e := visit(n.X, depth+1)
			if e != nil {
				return nil, e
			}
			y, e := visit(n.Y, depth+1)
			return map[string]any{"kind": "binary", "operator": n.Op.String(), "left": x, "right": y}, e
		case *ast.CallExpr:
			fn, ok := n.Fun.(*ast.Ident)
			if !ok || n.Ellipsis.IsValid() {
				return nil, fmt.Errorf("only named functions are supported")
			}
			arity, ok := formulaArity[fn.Name]
			if !ok {
				return nil, fmt.Errorf("unsupported v2 function %q", fn.Name)
			}
			if len(n.Args) < arity[0] || len(n.Args) > arity[1] {
				return nil, fmt.Errorf("%s accepts %d–%d arguments", fn.Name, arity[0], arity[1])
			}
			args := []any{}
			for _, a := range n.Args {
				x, e := visit(a, depth+1)
				if e != nil {
					return nil, e
				}
				args = append(args, x)
			}
			return map[string]any{"kind": "call", "function": fn.Name, "arguments": args}, nil
		}
		return nil, fmt.Errorf("unsupported expression node %T", expr)
	}
	encoded, e := visit(tree, 0)
	return encoded, deps, e
}
func formulaRegistry() any {
	names := []string{}
	for name := range formulaArity {
		names = append(names, name)
	}
	sort.Strings(names)
	functions := []any{}
	for _, name := range names {
		help := formulaV1Functions[name]
		since := 1
		if extra, ok := formulaV2Functions[name]; ok {
			help = extra
			since = 2
		}
		functions = append(functions, map[string]any{"name": name, "sinceVersion": since, "signature": help, "minArgs": formulaArity[name][0], "maxArgs": formulaArity[name][1]})
	}
	return map[string]any{"version": 2, "legacyDefaultVersion": 1, "functions": functions, "operators": []string{"+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "!"}, "resultTypes": []string{"null", "text", "number", "boolean", "date", "page", "person", "list"}, "limits": map[string]any{"expressionBytes": 8192, "depth": 48, "work": 1024, "listItems": 256, "resultBytes": 65536, "numberBits": 4096, "decimalPlaces": 12, "regexPatternBytes": 1024, "regexMatches": 1024}, "differences": []string{"No implicit text/boolean/number coercions; null differs from zero and false.", "Lists are homogeneous and contain scalar values, references or dates; nested lists/callbacks/member access are unsupported.", "Relations expose scoped page references; cross-record property traversal uses configured rollup properties.", "Calendar math uses the stored timezone and rejects nonexistent wall times; elapsed math uses instants.", "Regex uses RE2; replacements are literal, not capture substitutions.", "Imported formulas remain evaluator v1 unless explicitly edited; captured evidence is never treated as a live result."}}
}
