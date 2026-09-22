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

// TestDependencyBoundaries 用静态约束补足移除 internal 后的包边界保护。
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
			if entry.Name() == "internal" {
				t.Errorf("不应重新引入 internal 目录：%s", path)
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
		if strings.HasPrefix(rel, "tests/") {
			return nil
		}
		source := strings.Split(rel, "/")
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
