package classifier

import (
	"bytes"
	"text/template"
)

// DefaultSystemPrompt is the default template for the classification system prompt.
const DefaultSystemPrompt = `You are an email classifier. Analyze the given email and classify it into one of the following categories.

Categories:
{{range .Categories}}- {{.Name}}: {{.Description}}
{{end}}
Respond with a JSON object containing:
- "category": the category name (must be exactly one of the listed categories above)
- "confidence": a float between 0.0 and 1.0 indicating your confidence
- "reasoning": a brief explanation for your classification

Example response format:
{"category": "<CATEGORY_NAME>", "confidence": 0.95, "reasoning": "Brief explanation here."}`

// PromptData contains the data passed to the system prompt template.
type PromptData struct {
	Categories []Category
}

// RenderSystemPrompt renders the system prompt template with the given categories.
func RenderSystemPrompt(tmplStr string, categories []Category) (string, error) {
	tmpl, err := template.New("system").Parse(tmplStr)
	if err != nil {
		return "", err
	}

	data := PromptData{Categories: categories}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
