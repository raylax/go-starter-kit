package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDependencyBoundaries 约束 internal 内部依赖方向与测试辅助代码的使用位置。
func TestDependencyBoundaries(t *testing.T) {
	root := ".."
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 || fields[0] != "module" {
		t.Fatal("无法读取模块路径")
	}
	prefix := fields[1] + "/"
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// 集成测试共享夹具独立于应用实现，生产代码仍不能导入 tests。
		if strings.HasPrefix(rel, "tests/integration/testutil/") {
			return nil
		}
		if !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") {
			t.Errorf("应用实现应放入 internal，共享集成测试夹具应放入 tests/integration/testutil：%s", rel)
			return nil
		}
		source := strings.Split(strings.TrimPrefix(rel, "internal/"), "/")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "internal/modules/account/") {
			for _, violation := range accountStoreViolations(entry.Name(), file) {
				t.Errorf("%s: %s", rel, violation)
			}
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if (imported == "go.uber.org/fx" || strings.HasPrefix(imported, "go.uber.org/fx/")) && source[0] != "app" {
				t.Errorf("Fx 只能用于应用装配层：%s", rel)
			}
			if !strings.HasPrefix(imported, prefix) {
				continue
			}
			dependency := strings.TrimPrefix(imported, prefix)
			if !strings.HasPrefix(dependency, "internal/") {
				t.Errorf("%s 不应依赖 internal 外的应用包：%s", rel, dependency)
				continue
			}
			dependency = strings.TrimPrefix(dependency, "internal/")
			allowed := false
			switch source[0] {
			case "cmd":
				allowed = dependency == "app/"+source[1]
			case "app":
				allowed = dependency == "app/appfx" || dependency == "db" || dependency == "httpapi" || dependency == "identity" || dependency == "authorization" || strings.HasPrefix(dependency, "platform/") || (strings.HasPrefix(dependency, "modules/") && strings.Count(dependency, "/") == 1)
			case "httpapi":
				allowed = dependency == "identity" || dependency == "authorization" || dependency == "apperror" || dependency == "pagination"
			case "identity":
				allowed = dependency == "apperror"
			case "apperror", "pagination", "validation", "authorization":
				allowed = false
			case "db":
				allowed = dependency == "apperror"
			case "platform":
				allowed = strings.HasPrefix(dependency, "platform/")
			case "modules":
				allowed = moduleImportAllowed(entry.Name(), dependency)

			}
			if !allowed {
				t.Errorf("%s 不应依赖 %s", rel, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// 模块默认采用业务依赖集合，HTTP 和 DTO 是显式例外。
func moduleImportAllowed(name, dependency string) bool {
	if name == "model.go" || strings.HasSuffix(name, "_model.go") {
		return dependency == "apperror" || dependency == "authorization"
	}
	if name == "dto.go" || strings.HasSuffix(name, "_dto.go") {
		return dependency == "httpapi"
	}
	common := dependency == "identity" || dependency == "authorization" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
	if name == "http.go" || strings.HasSuffix(name, "_http.go") {
		return common || dependency == "httpapi"
	}
	return common || dependency == "db" || dependency == "db/sqlc"
}

func accountStoreViolations(name string, file *ast.File) []string {
	if name == "store.go" || strings.HasPrefix(name, "store_") {
		return nil
	}
	var violations []string
	aliases := map[string]bool{}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if strings.HasSuffix(path, "/internal/db/sqlc") {
			alias := "sqlc"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			aliases[alias] = true
			if alias == "." {
				violations = append(violations, "生成查询禁止点导入")
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "rawQueries" {
			violations = append(violations, "生成查询只能在 store 中访问")
		}
		if id, ok := sel.X.(*ast.Ident); ok && aliases[id.Name] && (sel.Sel.Name == "New" || sel.Sel.Name == "Queries") {
			violations = append(violations, "账户业务必须通过 store 构造和访问查询")
		}
		return true
	})
	return violations
}
