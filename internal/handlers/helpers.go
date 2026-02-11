package handlers

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"easybook/internal/types"
	"easybook/internal/utils"
	"easybook/internal/view"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func getSafeRedirectPath(value string, fallback string) string {
	pathValue := strings.TrimSpace(value)
	if !strings.HasPrefix(pathValue, "/") || strings.HasPrefix(pathValue, "//") {
		return fallback
	}
	return pathValue
}

func renderAuthControls(user *types.CurrentUser, nextPath string) string {
	if user != nil {
		return fmt.Sprintf(`
      <div class="auth-row">
        <span>
          Signed in as <strong>%s</strong>
        </span>
        <form method="POST" action="/logout" style="display:inline;">
          <input type="hidden" name="next" value="%s" />
          <button type="submit" class="btn btn-outline btn-small">Logout</button>
        </form>
      </div>
    `,
			view.EscapeHTML(user.Email),
			view.EscapeHTML(nextPath),
		)
	}

	return fmt.Sprintf(`
    <div class="auth-row">
      <span>You are browsing as a guest.</span>
      <div style="display:flex; gap:8px; flex-wrap:wrap;">
        <a class="btn btn-outline btn-small" href="/login?next=%s">Login</a>
        <a class="btn btn-outline btn-small" href="/register?next=%s">Register</a>
      </div>
    </div>
  `, url.QueryEscape(nextPath), url.QueryEscape(nextPath))
}

func renderPaginationBar(meta utils.PaginationMeta, basePath string, query map[string]string) string {
	if meta.TotalPages <= 1 {
		return ""
	}

	buildURL := func(page int) string {
		params := url.Values{}
		for key, value := range query {
			if strings.TrimSpace(value) == "" {
				continue
			}
			params.Set(key, value)
		}
		params.Set("page", fmt.Sprintf("%d", page))
		return basePath + "?" + params.Encode()
	}

	prevPart := `<span class="pagination-empty"></span>`
	if meta.HasPrev && meta.PrevPage != nil {
		prevPart = fmt.Sprintf(`<a class="btn btn-outline btn-small" href="%s">Previous</a>`, view.EscapeHTML(buildURL(*meta.PrevPage)))
	}

	nextPart := `<span class="pagination-empty"></span>`
	if meta.HasNext && meta.NextPage != nil {
		nextPart = fmt.Sprintf(`<a class="btn btn-outline btn-small" href="%s">Next</a>`, view.EscapeHTML(buildURL(*meta.NextPage)))
	}

	return fmt.Sprintf(`
    <div class="pagination-bar">
      %s
      <span>Page %d of %d (%d records)</span>
      %s
    </div>
  `,
		prevPart,
		meta.Page,
		meta.TotalPages,
		meta.Total,
		nextPart,
	)
}

func objectIDHex(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case primitive.ObjectID:
		return typed.Hex()
	case *primitive.ObjectID:
		if typed == nil {
			return ""
		}
		return typed.Hex()
	case string:
		text := strings.TrimSpace(typed)
		if text == "<nil>" {
			return ""
		}
		return text
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func stringValue(document map[string]any, key string) string {
	if document == nil {
		return ""
	}
	value, ok := document[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func floatValue(document map[string]any, key string) float64 {
	if document == nil {
		return 0
	}
	value, ok := document[key]
	if !ok {
		return 0
	}

	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}

	return 0
}

func intValue(document map[string]any, key string) int {
	if document == nil {
		return 0
	}
	value, ok := document[key]
	if !ok {
		return 0
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		if math.Mod(typed, 1) == 0 {
			return int(typed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}

	return 0
}

func stringSliceValue(document map[string]any, key string) []string {
	if document == nil {
		return []string{}
	}
	value, ok := document[key]
	if !ok {
		return []string{}
	}

	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return []string{}
		}
		return []string{text}
	}
}
