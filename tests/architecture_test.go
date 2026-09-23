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
		if !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") {
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
			case "testutil":
				// 公共夹具可以装配 HTTP 和数据库。
				allowed = len(source) == 2 && (dependency == "db" || dependency == "httpapi" || dependency == "identity")
			case "modules":
				generated := "db/sqlc"
				allowed = dependency == generated || dependency == "db" || dependency == "identity" || dependency == "authorization" || dependency == "httpapi" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
				if entry.Name() == "model.go" {
					allowed = dependency == "apperror" || dependency == "authorization"
				}
				if entry.Name() == "dto.go" || strings.HasSuffix(entry.Name(), "_dto.go") {
					allowed = dependency == "httpapi"
				}
				if source[1] == "account" && entry.Name() != "http.go" && !strings.HasSuffix(entry.Name(), "_http.go") && entry.Name() != "dto.go" && !strings.HasSuffix(entry.Name(), "_dto.go") && entry.Name() != "model.go" {
					allowed = dependency == generated || dependency == "db" || dependency == "identity" || dependency == "authorization" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
				}
				if entry.Name() == "service.go" {
					allowed = dependency == generated || dependency == "db" || dependency == "identity" || dependency == "authorization" || dependency == "apperror" || dependency == "pagination" || dependency == "validation"
				}
				if (entry.Name() == "http.go" || strings.HasSuffix(entry.Name(), "_http.go")) && (dependency == generated || dependency == "db") {
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
