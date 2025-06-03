package workflow

import "time"

// Workflow represents a workflow definition.
type Workflow struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Meta  MetaData   `json:"meta,omitempty"`
	Nodes []WorkflowNode `json:"nodes"`
}

// MetaData holds additional information about the workflow.
type MetaData struct {
	Description string `json:"description,omitempty"`
}

// WorkflowNode represents a single node in the workflow.
type WorkflowNode struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Name       string           `json:"name"`
	Next       string           `json:"next,omitempty"`
	Fields     []FormField      `json:"fields,omitempty"` // <--- ADDED: This will now unmarshal the "fields" array directly
	Script     *ScriptConfig    `json:"script,omitempty"`
	Conditions []GatewayCondition `json:"conditions,omitempty"`
	End        *EndConfig       `json:"end,omitempty"`
	Timeout    *TimeoutConfig   `json:"timeout,omitempty"`
	Signal     *SignalConfig    `json:"signal,omitempty"` // This field is crucial for signal handling
}

// FormField defines a single field within a form.
type FormField struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"` // Changed to omitempty as 'label' is not in your provided form JSON
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// ScriptConfig defines the structure for script nodes.
type ScriptConfig struct {
	Code string `json:"code"` // Base64 encoded JavaScript
}

// GatewayConfig defines the structure for gateway nodes.
type GatewayConfig struct {
	Conditions []GatewayCondition `json:"conditions"`
}

// GatewayCondition defines a single condition for a gateway.
type GatewayCondition struct {
	When   string `json:"when,omitempty"` // Base64 encoded JavaScript condition
	Next   string `json:"next"`
	Else   bool   `json:"else,omitempty"`
	Signal *SignalConfig `json:"signal,omitempty"` // Signal to throw on this path (optional)
}

// EndConfig defines the structure for end nodes.
type EndConfig struct {
	Signal *SignalConfig `json:"signal,omitempty"` // Signal to emit when ending (optional)
	HTML   string        `json:"html,omitempty"`   // HTML content for end nodes (from your JSON)
}

// TimeoutConfig defines timeout behavior for a node.
type TimeoutConfig struct {
	Duration string `json:"duration"` // e.g., "1m", "5s", "1h"
	Next     string `json:"next"`     // Node to transition to on timeout
}

// SignalConfig defines signals to catch, emit, or throw.
// Added 'Throw' field for consistency with gateway signal logic
type SignalConfig struct {
	Emit  string `json:"emit,omitempty"`  // Signal to emit (e.g., from end node)
	Catch string `json:"catch,omitempty"` // Signal to catch (e.g., at start node)
	Throw string `json:"throw,omitempty"` // Signal to throw (e.g., from gateway)
}

// WorkflowInstance represents a running instance of a workflow.
type WorkflowInstance struct {
	ID                      string                 // UUID for the overall instance
	WorkflowID              string                 // ID of the workflow definition this instance is based on
	CurrentNode             string                 // **DEFINITION ID** of the current node (e.g., "start_node", "task_form")
	CurrentNodeInstanceDBID string                 // **UUID from workflow_instance_nodes table** for the *specific execution* of the current node
	Context                 map[string]interface{} // Dynamic data passed through the workflow
	WaitingSignal           string                 // If not empty, instance is paused waiting for this signal
	ExpiresAt               *time.Time             // If set, instance will expire at this time
	CreatedAt               time.Time
	UpdatedAt               time.Time
	WorkflowDef             *Workflow              // Pointer to the loaded workflow definition
	CurrentNodeDef          *WorkflowNode          // Pointer to the current node's definition
}