package scripts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log" // Ensure log package is imported
	"reflect"

	"github.com/dop251/goja"
)

// setupConsole configures a basic 'console' object in the Goja VM
// that directs output to the Go application's log.
func setupConsole(vm *goja.Runtime) error {
    fmt.Println("--- DEBUG: setupConsole function is being called and new console is being set up! ---") // ADD THIS LINE

    console := vm.NewObject()

    // Implement console.log
    err := console.Set("log", func(call goja.FunctionCall) goja.Value {
        var args []interface{}
        for _, arg := range call.Arguments {
            args = append(args, arg.Export())
        }
        log.Println("[JS Log]", fmt.Sprint(args...))
        return goja.Undefined()
    })
    if err != nil {
        return fmt.Errorf("failed to set console.log: %w", err)
    }

    // Implement console.warn (optional)
    err = console.Set("warn", func(call goja.FunctionCall) goja.Value {
        var args []interface{}
        for _, arg := range call.Arguments {
            args = append(args, arg.Export())
        }
        log.Println("[JS Warn]", fmt.Sprint(args...))
        return goja.Undefined()
    })
    if err != nil {
        return fmt.Errorf("failed to set console.warn: %w", err)
    }

    // Implement console.error (optional)
    err = console.Set("error", func(call goja.FunctionCall) goja.Value {
        var args []interface{}
        for _, arg := range call.Arguments {
            args = append(args, arg.Export())
        }
        log.Println("[JS Error]", fmt.Sprint(args...))
        return goja.Undefined()
    })
    if err != nil {
        return fmt.Errorf("failed to set console.error: %w", err)
    }

    return vm.Set("console", console)
}

// ExecuteScript runs a base64 encoded JavaScript in a Goja VM.
// It takes initial context, executes the script, and returns the modified context.
func ExecuteScript(base64Script string, context map[string]interface{}) (map[string]interface{}, error) {
	decodedScript, err := base64.StdEncoding.DecodeString(base64Script)
	if err != nil {
		return nil, fmt.Errorf("error decoding base64 script: %w", err)
	}

	vm := goja.New()

	// Setup the console object in the VM
	if err := setupConsole(vm); err != nil {
		return nil, fmt.Errorf("failed to setup console in VM: %w", err)
	}

	// Convert Go map to Goja object
	contextObj := vm.NewObject()
	for k, v := range context {
		err = contextObj.Set(k, vm.ToValue(v))
		if err != nil {
			return nil, fmt.Errorf("failed to set context value for key %s: %w", k, err)
		}
	}

	// Set process_data in the VM
	err = vm.Set("process_data", contextObj)
	if err != nil {
		return nil, fmt.Errorf("failed to set process_data in VM: %w", err)
	}

	_, err = vm.RunString(string(decodedScript))
	if err != nil {
		return nil, fmt.Errorf("error executing script: %w", err)
	}

	// Get the modified process_data back from the VM
	val := vm.Get("process_data")
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return context, nil // No changes, return original
	}

	exported := val.Export()

	// Convert Goja exported value back to map[string]interface{}
	// This might require a more robust conversion for nested objects/arrays
	if newContext, ok := exported.(map[string]interface{}); ok {
		return newContext, nil
	} else if obj, ok := exported.(goja.Object); ok {
		// If it's still a Goja object, convert it properly
		newContext := make(map[string]interface{})
		for _, key := range obj.Keys() {
			newContext[key] = obj.Get(key).Export()
		}
		return newContext, nil
	}

	// Log if conversion is unexpected, but return existing context
	log.Printf("Warning: process_data after script execution is not a map[string]interface{}. Type: %v", reflect.TypeOf(exported))
	return context, nil
}

// EvaluateCondition runs a base64 encoded JavaScript condition in a Goja VM.
// It takes initial context and returns a boolean result.
func EvaluateCondition(base64Condition string, context map[string]interface{}) (bool, error) {
	decodedCondition, err := base64.StdEncoding.DecodeString(base64Condition)
	if err != nil {
		return false, fmt.Errorf("error decoding base64 condition: %w", err)
	}

	vm := goja.New()

	// Setup the console object in the VM for condition evaluation too
	if err := setupConsole(vm); err != nil {
		return false, fmt.Errorf("failed to setup console in VM for condition: %w", err)
	}

	// Convert Go map to Goja object
	contextObj := vm.NewObject()
	for k, v := range context {
		err = contextObj.Set(k, vm.ToValue(v))
		if err != nil {
			return false, fmt.Errorf("failed to set context value for key %s: %w", k, err)
		}
	}
	err = vm.Set("process_data", contextObj)
	if err != nil {
		return false, fmt.Errorf("failed to set process_data in VM for condition: %w", err)
	}

	val, err := vm.RunString(string(decodedCondition))
	if err != nil {
		return false, fmt.Errorf("error evaluating condition script: %w", err)
	}

	if goja.IsUndefined(val) || goja.IsNull(val) {
		return false, nil // Treat undefined/null as false
	}

	if result, ok := val.Export().(bool); ok {
		return result, nil
	}
	return false, fmt.Errorf("condition script did not return a boolean value")
}

// Convert a Go map to a JSON string
func ToJSON(data map[string]interface{}) (string, error) {
    b, err := json.Marshal(data)
    if err != nil {
        return "", err
    }
    return string(b), nil
}

// Convert a JSON string to a Go map
func FromJSON(jsonStr string) (map[string]interface{}, error) {
    var data map[string]interface{}
    err := json.Unmarshal([]byte(jsonStr), &data)
    return data, err
}