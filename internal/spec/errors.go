package spec

import "strings"

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}

	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Field+": "+item.Message)
	}

	return strings.Join(parts, "; ")
}
