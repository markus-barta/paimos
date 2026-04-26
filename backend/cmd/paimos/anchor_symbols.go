package main

import (
	"fmt"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type anchorSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Language  string `json:"language"`
}

type symbolLanguage struct {
	name  string
	lang  *tree_sitter.Language
	kinds map[string]bool
}

func languageForPath(path string) *symbolLanguage {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return &symbolLanguage{
			name: "go",
			lang: tree_sitter.NewLanguage(tree_sitter_go.Language()),
			kinds: map[string]bool{
				"function_declaration": true,
				"method_declaration":   true,
				"type_declaration":     true,
				"type_spec":            true,
			},
		}
	case ".js", ".jsx":
		return &symbolLanguage{
			name: "javascript",
			lang: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
			kinds: map[string]bool{
				"function_declaration": true,
				"method_definition":    true,
				"class_declaration":    true,
				"generator_function":   true,
			},
		}
	case ".ts":
		return &symbolLanguage{
			name: "typescript",
			lang: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
			kinds: map[string]bool{
				"function_declaration":   true,
				"method_definition":      true,
				"class_declaration":      true,
				"interface_declaration":  true,
				"type_alias_declaration": true,
				"enum_declaration":       true,
			},
		}
	case ".tsx":
		return &symbolLanguage{
			name: "tsx",
			lang: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
			kinds: map[string]bool{
				"function_declaration":   true,
				"method_definition":      true,
				"class_declaration":      true,
				"interface_declaration":  true,
				"type_alias_declaration": true,
				"enum_declaration":       true,
			},
		}
	default:
		return nil
	}
}

func detectAnchorSymbol(relPath string, content []byte, line int) any {
	lang := languageForPath(relPath)
	if lang == nil {
		return nil
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang.lang)
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	match := findEnclosingSymbol(root, content, line, lang.kinds)
	if match == nil {
		return nil
	}
	name := extractSymbolName(match, content)
	if name == "" {
		name = match.Kind()
	}
	start := int(match.StartPosition().Row) + 1
	end := int(match.EndPosition().Row) + 1
	if end < start {
		end = start
	}
	return anchorSymbol{
		Name:      name,
		Kind:      normalizeSymbolKind(match.Kind()),
		StartLine: start,
		EndLine:   end,
		Language:  lang.name,
	}
}

func findEnclosingSymbol(node *tree_sitter.Node, source []byte, line int, kinds map[string]bool) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	start := int(node.StartPosition().Row) + 1
	end := int(node.EndPosition().Row) + 1
	if line < start || line > end {
		return nil
	}
	var best *tree_sitter.Node
	if kinds[node.Kind()] {
		best = node
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if candidate := findEnclosingSymbol(child, source, line, kinds); candidate != nil {
			best = candidate
		}
	}
	return best
}

func extractSymbolName(node *tree_sitter.Node, source []byte) string {
	for _, field := range []string{"name", "declarator"} {
		if named := node.ChildByFieldName(field); named != nil {
			if name := strings.TrimSpace(named.Utf8Text(source)); name != "" {
				return trimSymbolName(name)
			}
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "identifier", "property_identifier", "type_identifier":
			if name := strings.TrimSpace(child.Utf8Text(source)); name != "" {
				return trimSymbolName(name)
			}
		}
	}
	return ""
}

func trimSymbolName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "func ")
	v = strings.TrimPrefix(v, "type ")
	v = strings.TrimPrefix(v, "class ")
	v = strings.TrimPrefix(v, "interface ")
	return strings.TrimSpace(v)
}

func normalizeSymbolKind(kind string) string {
	switch kind {
	case "function_declaration", "generator_function":
		return "function"
	case "method_declaration", "method_definition":
		return "method"
	case "class_declaration":
		return "class"
	case "interface_declaration":
		return "interface"
	case "type_alias_declaration", "type_declaration", "type_spec":
		return "type"
	case "enum_declaration":
		return "enum"
	default:
		return kind
	}
}

func describeAnchorSymbol(v any) string {
	sym, ok := v.(anchorSymbol)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %s (%s:%d-%d)", sym.Kind, sym.Name, sym.Language, sym.StartLine, sym.EndLine)
}
