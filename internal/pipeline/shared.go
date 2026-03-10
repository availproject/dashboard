package pipeline

import (
	"encoding/json"
	"strings"

	"github.com/your-org/dashboard/internal/store"
)

// buildPrompt constructs the shared prompt wrapper used by all pipelines:
//
//	{prompt_wrapper.txt} <schema>...</schema> [<instructions>...</instructions>]
//	[<annotations>...</annotations>] <inputs>...</inputs>
//
// If inputs contains an "instructions" key its value is lifted into a dedicated
// <instructions> block (plain text, not JSON-escaped) and omitted from <inputs>.
func buildPrompt(schema string, inputs map[string]any, annotations []store.Annotation) string {
	// Lift instructions out of inputs into its own block.
	instructions := ""
	dataInputs := inputs
	if raw, ok := inputs["instructions"]; ok {
		if s, ok := raw.(string); ok {
			instructions = strings.TrimSpace(s)
		}
		dataInputs = make(map[string]any, len(inputs)-1)
		for k, v := range inputs {
			if k != "instructions" {
				dataInputs[k] = v
			}
		}
	}

	inputsJSON, _ := json.Marshal(dataInputs)

	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(promptWrapper))
	sb.WriteString(" <schema>")
	sb.WriteString(schema)
	sb.WriteString("</schema>")

	if instructions != "" {
		sb.WriteString(" <instructions>")
		sb.WriteString(instructions)
		sb.WriteString("</instructions>")
	}

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
