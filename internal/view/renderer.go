package view

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SafeHTML string

type Renderer struct {
	viewsDir string
}

func NewRenderer(viewsDir string) *Renderer {
	return &Renderer{viewsDir: viewsDir}
}

func Safe(html string) SafeHTML {
	return SafeHTML(html)
}

func EscapeHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

func (r *Renderer) Render(fileName string, replacements map[string]any) (string, error) {
	fullPath := filepath.Join(r.viewsDir, fileName)
	bytes, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	html := string(bytes)

	for key, rawValue := range replacements {
		value := ""
		switch typed := rawValue.(type) {
		case SafeHTML:
			value = string(typed)
		case nil:
			value = ""
		default:
			value = EscapeHTML(fmt.Sprint(rawValue))
		}
		html = strings.ReplaceAll(html, "{{"+key+"}}", value)
	}

	return html, nil
}
