package workspace

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/scanner"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

// Comment-only proposals for canonical Go blocks may change layout and ordinary
// comments, but cannot change a parseable file/declaration's syntax tree. This is
// a proposal guard, not code execution, type checking, or a generation repair.
func validateAIGoCommentChange(language, before, after string) error {
	if language = strings.ToLower(strings.TrimSpace(language)); language != "go" && language != "golang" {
		return nil
	}
	parse := func(source string) (*ast.File, error) {
		return parser.ParseFile(token.NewFileSet(), "comment.go", source, parser.SkipObjectResolution)
	}
	prefix := ""
	original, err := parse(before)
	if err != nil {
		prefix = "package commentdraft\n"
		original, err = parse(prefix + before)
	}
	if err != nil {
		// Existing incomplete or non-declaration snippets retain their manual
		// review path. The guard makes no syntax guarantee for those sources.
		return nil
	}
	replacement, err := parse(prefix + after)
	if err != nil {
		return Fail("INVALID_REQUEST", "The comment draft is not valid Go syntax. Edit or regenerate it before reviewing the replacement; the source was preserved.")
	}
	if before != after && (aiGoSemanticComments(before, original) || aiGoSemanticComments(after, replacement)) {
		return Fail("INVALID_REQUEST", "Go compiler directives and cgo comments require manual source editing; a comment-only AI replacement cannot safely change them.")
	}
	// token.Pos also encodes syntax presence (for example variadic call
	// ellipses and type aliases). Keep missing versus present, erase offsets.
	for _, tree := range []*ast.File{original, replacement} {
		ast.Inspect(tree, func(node ast.Node) bool {
			if node == nil {
				return true
			}
			value := reflect.ValueOf(node).Elem()
			for i := 0; i < value.NumField(); i++ {
				field := value.Field(i)
				if field.CanSet() && field.Type() == reflect.TypeOf(token.Pos(0)) && field.Int() != 0 {
					field.SetInt(1)
				}
			}
			return true
		})
	}
	if !reflect.DeepEqual(original, replacement) {
		return Fail("INVALID_REQUEST", "The comment draft changes Go code beyond comments or layout. Edit or regenerate it before reviewing the replacement; the source was preserved.")
	}
	return nil
}

// Compiler directives and cgo preambles are not ordinary comments. Reject their
// mutation conservatively instead of guessing whether a move changes behavior.
func aiGoSemanticComments(source string, file *ast.File) bool {
	for _, imported := range file.Imports {
		if path, err := strconv.Unquote(imported.Path.Value); err == nil && path == "C" {
			return true
		}
	}
	fset := token.NewFileSet()
	var lexer scanner.Scanner
	lexer.Init(fset.AddFile("comments.go", -1, len(source)), []byte(source), nil, scanner.ScanComments)
	for {
		_, kind, literal := lexer.Scan()
		if kind == token.EOF {
			return false
		}
		if kind == token.COMMENT {
			if constraint.IsPlusBuild(literal) {
				return true
			}
			for _, prefix := range []string{"//go:", "//line ", "/*line "} {
				if strings.HasPrefix(literal, prefix) {
					return true
				}
			}
		}
	}
}
