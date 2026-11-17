package process

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A new direct exec call would silently work in Linux CI and fail in Termux.
// Keep platform adaptation at the shared boundary rather than patching callers.
func TestChildCommandsUsePlatformAdapter(t *testing.T) {
	root := filepath.Join("..", "..")
	files := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_windows.go") {
			return nil
		}
		if filepath.Dir(path) == filepath.Join(root, "internal", "process") {
			return nil
		}
		source, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		aliases := map[string]bool{}
		for _, imported := range source.Imports {
			value, _ := strconv.Unquote(imported.Path.Value)
			if value == "os/exec" {
				name := "exec"
				if imported.Name != nil {
					name = imported.Name.Name
				}
				aliases[name] = true
			}
		}
		ast.Inspect(source, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Command" && selector.Sel.Name != "CommandContext") {
				return true
			}
			if name, ok := selector.X.(*ast.Ident); ok && aliases[name.Name] {
				t.Errorf("%s bypasses process.Command/CommandContext", files.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
