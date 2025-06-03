// workflow/gateway.go
package workflow

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// getNestedValue safely retrieves a nested value from a map[string]interface{}.
// It traverses paths like "process_data.user_age".
func getNestedValue(m map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			val, ok := current[part]
			return val, ok
		}
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

// compareNumbers performs a comparison between two float64 values based on the operator.
func compareNumbers(actual, target float64, op string) (bool, error) {
	switch op {
	case ">=":
		return actual >= target, nil
	case "<=":
		return actual <= target, nil
	case "==":
		return actual == target, nil
	case ">":
		return actual > target, nil
	case "<":
		return actual < target, nil
	case "!=":
		return actual != target, nil
	default:
		return false, fmt.Errorf("unsupported numeric operator: %s", op)
	}
}

// compareStrings performs a lexicographical comparison between two string values based on the operator.
func compareStrings(actual, target string, op string) (bool, error) {
	switch op {
	case "==":
		return actual == target, nil
	case "!=":
		return actual != target, nil
	case ">":
		return actual > target, nil
	case "<":
		return actual < target, nil
	case ">=":
		return actual >= target, nil
	case "<=":
		return actual <= target, nil
	default:
		return false, fmt.Errorf("unsupported string operator: %s", op)
	}
}

// evaluateSimpleCondition evaluates a simple comparison expression
// (e.g., "variable.path >= value") against the provided context.
// It supports both number and string comparisons.
func evaluateSimpleCondition(condition string, context map[string]interface{}) (bool, error) {
	if condition == "" {
		return false, fmt.Errorf("empty condition string provided")
	}

	// Order matters: check multi-character operators first
	operators := []string{">=", "<=", "==", "!=", ">", "<"}
	var op string
	var parts []string
	foundOp := false

	for _, o := range operators {
		if strings.Contains(condition, o) {
			op = o
			parts = strings.SplitN(condition, op, 2) // Split only once
			foundOp = true
			break
		}
	}

	if !foundOp || len(parts) != 2 {
		return false, fmt.Errorf("unsupported condition format or missing operator: %s", condition)
	}

	variablePath := strings.TrimSpace(parts[0])
	targetValueStr := strings.TrimSpace(parts[1])

	actualValue, ok := getNestedValue(context, variablePath)
	if !ok {
		return false, fmt.Errorf("variable '%s' not found in context", variablePath)
	}

	// Attempt to parse the target value string as a number
	targetNum, targetErr := strconv.ParseFloat(targetValueStr, 64)

	// Determine type of actual value and proceed with comparison
	switch v := actualValue.(type) {
	case float64:
		if targetErr == nil { // Both are numbers
			return compareNumbers(v, targetNum, op)
		}
		// Actual is number, target string cannot be parsed as a number.
		// Comparison is not meaningful.
		return false, fmt.Errorf("type mismatch: cannot compare number with non-numeric string '%s' for variable '%s'", targetValueStr, variablePath)

	case int: // Handle int values from context by converting to float64
		if targetErr == nil { // Both are numbers
			return compareNumbers(float64(v), targetNum, op)
		}
		// Actual is number, target string cannot be parsed as a number.
		return false, fmt.Errorf("type mismatch: cannot compare number with non-numeric string '%s' for variable '%s'", targetValueStr, variablePath)

	case string:
		// Both actual and target are strings. Perform string comparison.
		// Note: Even if targetValueStr could be a number, if actualValue is a string,
		// we treat the comparison as a string comparison.
		return compareStrings(v, targetValueStr, op)

	// Add more cases here if you expect other types in your context (e.g., bool, []interface{})
	// For example, to compare booleans for equality:
	/*
		case bool:
			targetBool, err := strconv.ParseBool(targetValueStr)
			if err != nil {
				return false, fmt.Errorf("type mismatch: cannot compare boolean with non-boolean string '%s'", targetValueStr)
			}
			switch op {
			case "==": return v == targetBool, nil
			case "!=": return v != targetBool, nil
			default: return false, fmt.Errorf("unsupported boolean operator: %s", op)
			}
	*/

	default:
		return false, fmt.Errorf("unsupported variable type for comparison: %T for variable '%s'", actualValue, variablePath)
	}
}

// ResolveGatewayConditions evaluates the conditions of a gateway node
// and returns the ID of the next node to transition to, and any signal to throw.
func ResolveGatewayConditions(instance *WorkflowInstance) (string, string, error) {
	log.Printf("Resolving gateway conditions for node %s (instance %s)", instance.CurrentNode, instance.ID)

	conditions := instance.CurrentNodeDef.Conditions
	if len(conditions) == 0 {
		return "", "", fmt.Errorf("gateway node %s has no conditions defined for instance %s", instance.CurrentNode, instance.ID)
	}

	nextNodeID := ""
	signalToThrow := ""

	for _, condition := range conditions {
		var conditionMet bool

		if condition.When != "" {
			result, evalErr := evaluateSimpleCondition(condition.When, instance.Context)
			if evalErr != nil {
				log.Printf("Warning: Error evaluating gateway condition '%s' for node %s, instance %s: %v", condition.When, instance.CurrentNode, instance.ID, evalErr)
				continue
			}
			conditionMet = result
		} else if condition.Else {
			conditionMet = true
		}

		if conditionMet {
			nextNodeID = condition.Next
			if condition.Signal != nil && condition.Signal.Throw != "" {
				signalToThrow = condition.Signal.Throw
			}
			break // Exit loop on first matched condition
		}
	}

	if nextNodeID == "" {
		return "", "", fmt.Errorf("no matching gateway condition found for node %s, instance %s", instance.CurrentNode, instance.ID)
	}

	log.Printf("Gateway %s (instance %s) resolved next node to: %s", instance.CurrentNode, instance.ID, nextNodeID)
	return nextNodeID, signalToThrow, nil
}