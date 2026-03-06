package pipeline

import (
	"encoding/json"
	"strings"

	"github.com/your-org/dashboard/internal/store"
)

// buildPrompt constructs the shared prompt wrapper used by all pipelines:
//
//	You are analyzing a software team's status data. Respond ONLY with valid JSON
//	matching this schema: <schema>...</schema> [<annotations>...</annotations>]
//	<inputs>...</inputs>
func buildPrompt(schema string, inputs map[string]any, annotations []store.Annotation) string {
	inputsJSON, _ := json.Marshal(inputs)

	var sb strings.Builder
	sb.WriteString("You are analyzing a software team's status data. Respond ONLY with valid JSON matching this schema: <schema>")
	sb.WriteString(schema)
	sb.WriteString("</schema>")

	if len(annotations) > 0 {
		annotJSON, _ := json.Marshal(annotations)
		sb.WriteString(" <annotations>")
		sb.Write(annotJSON)
		sb.WriteString("</annotations>")
	}

	sb.WriteString(" <inputs>")
	sb.Write(inputsJSON)
	sb.WriteString("</inputs>")

	return sb.String()
}
