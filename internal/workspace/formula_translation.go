package workspace

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// TranslateImportedFormula translates only the published local subset. Callers
// must retain the original expression and captured value even on success. The
// result is never evidence that an imported cached value was recalculated.
func TranslateImportedFormula(original string, propertyIDs map[string]string) (string, error) {
	if len(original) > 8192 {
		return "", fmt.Errorf("formula exceeds 8192 bytes")
	}
	fset := token.NewFileSet()
	file := fset.AddFile("imported", -1, len(original))
	var lex scanner.Scanner
	var scanErr error
	lex.Init(file, []byte(original), func(_ token.Position, msg string) { scanErr = fmt.Errorf("unsupported source syntax: %s", msg) }, 0)
	var tokens []string
	for {
		_, kind, value := lex.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON && value == "\n" {
			continue
		}
		if kind == token.IF {
			value = "ifElse"
		}
		if value == "" {
			value = kind.String()
		}
		tokens = append(tokens, value)
	}
	if scanErr != nil {
		return "", scanErr
	}
	tree, err := parser.ParseExpr(strings.Join(tokens, " "))
	if err != nil {
		return "", fmt.Errorf("unsupported source formula syntax: %w", err)
	}
	functions := map[string]bool{"prop": true, "ifElse": true, "now": true, "today": true, "empty": true, "length": true, "lower": true, "upper": true, "contains": true, "concat": true, "abs": true, "round": true, "min": true, "max": true}
	var unsupported error
	ast.Inspect(tree, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || !functions[fn.Name] {
			unsupported = fmt.Errorf("source function is outside the supported compatibility subset")
			return false
		}
		if fn.Name == "prop" {
			if len(call.Args) != 1 {
				unsupported = fmt.Errorf("prop requires one property name")
				return false
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				unsupported = fmt.Errorf("dynamic source property references are unsupported")
				return false
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				unsupported = err
				return false
			}
			id, ok := propertyIDs[name]
			if !ok {
				unsupported = fmt.Errorf("source property %q has no stable local mapping", name)
				return false
			}
			lit.Value = strconv.Quote(id)
		}
		return true
	})
	if unsupported != nil {
		return "", unsupported
	}
	var out bytes.Buffer
	if err = format.Node(&out, fset, tree); err != nil {
		return "", err
	}
	return out.String(), nil
}
