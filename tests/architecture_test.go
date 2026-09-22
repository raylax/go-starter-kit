package tests

import (
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
		if rel != "main.go" && !strings.HasPrefix(rel, "internal/") {
			t.Errorf("应用实现与测试辅助代码应放入 internal：%s", rel)
			return nil
		}
		source := strings.Split(strings.TrimPrefix(rel, "internal/"), "/")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
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
			case "main.go":
				allowed = dependency == "app" || dependency == "db"
			case "app":
				allowed = dependency == "db" || dependency == "httpapi" || dependency == "identity" || strings.HasPrefix(dependency, "platform/") || (strings.HasPrefix(dependency, "modules/") && strings.Count(dependency, "/") == 1)
			case "httpapi":
				allowed = dependency == "identity" || dependency == "apperror" || dependency == "pagination"
			case "identity", "apperror", "pagination", "validation":
				allowed = false
			case "db":
				allowed = dependency == "apperror"
			case "platform":
				allowed = strings.HasPrefix(dependency, "platform/")
			case "testutil":
				// 公共夹具可以装配 HTTP 和数据库；JWT 子包保持独立。
				allowed = len(source) == 2 && (dependency == "db" || dependency == "httpapi" || dependency == "identity")
			case "modules":
				generated := "db/sqlc"
				allowed = dependency == generated || dependency == "db" || dependency == "identity" || dependency == "httpapi" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
				if entry.Name() == "model.go" {
					allowed = dependency == "apperror"
				}
				if entry.Name() == "dto.go" {
					allowed = dependency == "httpapi"
				}
				if entry.Name() == "service.go" {
					allowed = dependency == generated || dependency == "db" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
				}
				if entry.Name() == "http.go" && (dependency == generated || dependency == "db") {
					allowed = false
				}
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
