package workflow

import (
	"fmt"
	"html/template"
	"strings"
)

// GenerateHTMLForm generates an HTML form from a slice of FormField and prepopulates it with context.
// It now takes []FormField directly instead of *FormConfig.
func GenerateHTMLForm(formFields []FormField, context map[string]interface{}, instanceID string, errors map[string]string) (template.HTML, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<form action="/form/%s" method="POST">`, instanceID))
	sb.WriteString(`<table>`) // Use a table for better alignment, or div/flexbox for modern styling

	for _, field := range formFields { // Loop directly over formFields
		fieldName := field.Name
		fieldValue := ""
		if val, ok := context[fieldName]; ok {
			fieldValue = fmt.Sprintf("%v", val) // Convert any type to string
		}
		requiredAttr := ""
		if field.Required {
			requiredAttr = "required"
		}
		errorMsg := errors[fieldName]

		sb.WriteString(`<tr>`)
		// Using field.Label if available, otherwise default to capitalized fieldName
		label := field.Label
		if label == "" {
			label = strings.Title(fieldName)
		}
		sb.WriteString(fmt.Sprintf(`<td><label for="%s">%s:</label></td>`, fieldName, template.HTMLEscapeString(label))) // Use template.HTMLEscapeString for label
		sb.WriteString(`<td>`)
		switch field.Type {
		case "text", "number", "email":
			sb.WriteString(fmt.Sprintf(`<input type="%s" id="%s" name="%s" value="%s" %s>`,
				field.Type, fieldName, fieldName, template.HTMLEscapeString(fieldValue), requiredAttr))
		case "textarea":
			sb.WriteString(fmt.Sprintf(`<textarea id="%s" name="%s" %s>%s</textarea>`,
				fieldName, fieldName, requiredAttr, template.HTMLEscapeString(fieldValue)))
		// Add more input types as needed (checkbox, radio, select)
		default:
			sb.WriteString(fmt.Sprintf(`<input type="text" id="%s" name="%s" value="%s" %s>`,
				field.Type, fieldName, fieldName, template.HTMLEscapeString(fieldValue), requiredAttr)) // Default to field.Type, not just "text"
		}
		if errorMsg != "" {
			sb.WriteString(fmt.Sprintf(`<span style="color: red;">%s</span>`, template.HTMLEscapeString(errorMsg)))
		}
		sb.WriteString(`</td>`)
		sb.WriteString(`</tr>`)
	}
	sb.WriteString(`</table>`)
	sb.WriteString(`<br><button type="submit">Submit</button>`)
	sb.WriteString(`</form>`)

	return template.HTML(sb.String()), nil
}

// ValidateFormInput validates form input against a slice of FormField.
// Returns a map of errors (field name -> error message) if validation fails.
func ValidateFormInput(formFields []FormField, input map[string]string) map[string]string { // Loop directly over formFields
	errors := make(map[string]string)

	for _, field := range formFields {
		value, exists := input[field.Name]

		if field.Required && (!exists || strings.TrimSpace(value) == "") {
			errors[field.Name] = "This field is required."
			continue // Don't check type if required field is missing
		}

		if exists && strings.TrimSpace(value) != "" { // Only validate type if value is present
			switch field.Type {
			case "number":
				_, err := fmt.Sscanf(value, "%f", new(float64)) // Check if it's a valid number
				if err != nil {
					errors[field.Name] = "Must be a valid number."
				}
			case "email":
				// Basic email validation (can be enhanced with regex)
				if !strings.Contains(value, "@") || !strings.Contains(value, ".") {
					errors[field.Name] = "Must be a valid email address."
				}
			// Add more type validations as needed
			}
		}
	}
	return errors
}

// MergeFormInputIntoContext merges validated form input into the workflow context.
// Input map values are string, context values can be various types based on form field type.
func MergeFormInputIntoContext(context map[string]interface{}, formFields []FormField, input map[string]string) { // Loop directly over formFields
	for _, field := range formFields {
		if val, ok := input[field.Name]; ok {
			switch field.Type {
			case "number":
				var num float64
				if _, err := fmt.Sscanf(val, "%f", &num); err == nil {
					context[field.Name] = num
				} else {
					context[field.Name] = val // Keep as string if conversion fails, or handle error
				}
			case "text", "email", "textarea":
				context[field.Name] = val
			default:
				context[field.Name] = val
			}
		}
	}
}