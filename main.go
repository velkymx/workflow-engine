package main

import (
	"context"
	"database/sql" // Added for sql.ErrNoRows check
	"encoding/json"
	"fmt"
	"html/template" // RE-ADDED: Needed for rendering HTML forms and end node content
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath" // Used for filepath.Base
	"strings"
	"syscall"
	"time"

	"jbpmn-engine/db"
	"jbpmn-engine/workflow" // Ensure this is the correct path to your workflow package
)

// APIResponse defines the structure for all API JSON responses.
type APIResponse struct {
	InstanceID    string                 `json:"instance_id,omitempty"`
	WorkflowID    string                 `json:"workflow_id,omitempty"`
	CurrentNode   string                 `json:"current_node,omitempty"`
	Message       string                 `json:"message"`
	StatusURL     string                 `json:"status_url,omitempty"`
	FormURL       string                 `json:"form_url,omitempty"` // New field for form URLs
	Error         string                 `json:"error,omitempty"`
	Context       map[string]interface{} `json:"context,omitempty"`      // For status endpoint
	WaitingSignal string                 `json:"waiting_signal,omitempty"` // For status endpoint
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`     // For status endpoint
	FormFields    []workflow.FormField   `json:"form_fields,omitempty"`    // For GET /form/{instance_id} - still useful for client API usage
}

func main() {
	log.Println("Starting jBPMN Engine...")

	// Initialize the database
	err := db.InitDB("./jbpmn.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Database initialized successfully.")
	// Ensure DB is closed on exit, handling potential error
	defer func() {
		if err := db.CloseDB(); err != nil {
			log.Printf("Error closing DB: %v", err)
		}
	}()

	// Set workflow directory and load workflows from it
	workflowDir := "./workflows/" // This directory should be relative to where you run `go run main.go`
	workflow.SetWorkflowDirectory(workflowDir) // Set the directory in the workflow package
	err = workflow.LoadWorkflowsFromDir(workflowDir) // Load workflows from the directory into memory
	if err != nil {
		log.Fatalf("Failed to load workflow definitions from %s: %v", workflowDir, err)
	}
	log.Printf("Workflows loaded from %s.", workflowDir)

	// Setup HTTP server
	http.HandleFunc("/start/", startWorkflowHandler)
	http.HandleFunc("/signal/", signalWorkflowHandler)   // Handler for emitting signals
	http.HandleFunc("/status/", getWorkflowStatusHandler) // New handler for getting workflow status
	http.HandleFunc("/form/", submitFormHandler)         // Handler for getting form definition and submitting form data

	server := &http.Server{
		Addr: ":8080",
		// Recommended timeouts for production readiness
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// Start server in a goroutine so it doesn't block the main thread
	go func() {
		log.Printf("HTTP server starting on %s", server.Addr)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to start: %v", serveErr)
		}
	}()

	// Setup graceful shutdown: Listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan // Block until a shutdown signal is received
	log.Println("Received shutdown signal. Shutting down gracefully...")

	// Create a context with a timeout for server shutdown
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown() // Ensure the context is cancelled

	// Attempt to gracefully shut down the HTTP server
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		log.Fatalf("HTTP server forced to shutdown: %v", shutdownErr)
	}
	log.Println("HTTP server shut down.")

	log.Println("jBPMN Engine stopped.")
	fmt.Println("Application exited.")
}

// sendJSONResponse is a helper to standardize JSON responses.
func sendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error writing JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// startWorkflowHandler handles requests to start a new workflow instance.
func startWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		sendJSONResponse(w, http.StatusMethodNotAllowed, APIResponse{
			Error:   "Method not allowed. Use GET or POST.",
			Message: "Invalid HTTP method.",
		})
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 || pathParts[2] == "" {
		sendJSONResponse(w, http.StatusBadRequest, APIResponse{
			Error:   "Workflow ID not provided. Usage: /start/{workflowID}",
			Message: "Missing workflow ID.",
		})
		return
	}
	workflowID := pathParts[2]

	log.Printf("Attempting to create new instance for workflow ID: %s via HTTP request.", workflowID)

	instance, err := workflow.CreateNewInstance(workflowID)
	if err != nil {
		log.Printf("Error creating workflow instance for %s: %v", workflowID, err)
		sendJSONResponse(w, http.StatusInternalServerError, APIResponse{
			Error:   fmt.Sprintf("Failed to create workflow instance: %v", err),
			Message: "Failed to start workflow.",
		})
		return
	}

	response := APIResponse{
		InstanceID:  instance.ID,
		WorkflowID:  instance.WorkflowID,
		CurrentNode: instance.CurrentNode,
		StatusURL:   fmt.Sprintf("/status/%s", instance.ID),
	}

	if instance.WaitingSignal != "" {
		response.Message = fmt.Sprintf("Workflow instance created. Waiting for signal: '%s'.", instance.WaitingSignal)
	} else if instance.CurrentNodeDef != nil && instance.CurrentNodeDef.Type == "form" {
		// If the workflow immediately lands on a form node, provide the form URL
		response.Message = "Workflow instance created. Awaiting form submission."
		response.FormURL = fmt.Sprintf("/form/%s", instance.ID)
	} else {
		response.Message = "Workflow instance created and started execution."
	}

	sendJSONResponse(w, http.StatusOK, response)
	log.Printf("Successfully responded for workflow %s, instance %s", workflowID, instance.ID)
}

// signalWorkflowHandler handles requests to emit a signal to waiting workflows.
func signalWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONResponse(w, http.StatusMethodNotAllowed, APIResponse{
			Error:   "Method not allowed. Use GET.",
			Message: "Invalid HTTP method.",
		})
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 || pathParts[2] == "" {
		sendJSONResponse(w, http.StatusBadRequest, APIResponse{
			Error:   "Signal name not provided. Usage: /signal/{signalName}",
			Message: "Missing signal name.",
		})
		return
	}
	signalName := pathParts[2]

	log.Printf("Received signal: %s via HTTP request. Attempting to resume workflows...", signalName)

	err := workflow.EmitSignal(signalName)
	if err != nil {
		log.Printf("Error emitting signal %s: %v", signalName, err)
		sendJSONResponse(w, http.StatusInternalServerError, APIResponse{
			Error:   fmt.Sprintf("Failed to emit signal: %v", err),
			Message: "Failed to process signal.",
		})
		return
	}

	sendJSONResponse(w, http.StatusOK, APIResponse{
		Message: fmt.Sprintf("Signal '%s' processed. Attempting to resume workflows.", signalName),
	})
	log.Printf("Successfully responded for signal %s", signalName)
}

// getWorkflowStatusHandler retrieves the current status of a workflow instance.
func getWorkflowStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONResponse(w, http.StatusMethodNotAllowed, APIResponse{
			Error:   "Method not allowed. Use GET.",
			Message: "Invalid HTTP method.",
		})
		return
	}

	instanceID := filepath.Base(r.URL.Path) // Use filepath.Base for cleaner extraction
	if instanceID == "status" || instanceID == "" {
		sendJSONResponse(w, http.StatusBadRequest, APIResponse{
			Error:   "Instance ID not provided. Usage: /status/{instanceID}",
			Message: "Missing instance ID.",
		})
		return
	}

	instance, err := workflow.GetInstanceAndDefinition(instanceID)
	if err != nil {
		if err == sql.ErrNoRows { // Check for specific "not found" error from the DB
			sendJSONResponse(w, http.StatusNotFound, APIResponse{
				Error:   fmt.Sprintf("Workflow instance '%s' not found.", instanceID),
				Message: "Instance not found.",
			})
		} else {
			log.Printf("Error getting workflow instance status %s: %v", instanceID, err)
			sendJSONResponse(w, http.StatusInternalServerError, APIResponse{
				Error:   fmt.Sprintf("Failed to get instance status: %v", err),
				Message: "Failed to retrieve status.",
			})
		}
		return
	}

	// If the current node is an "end" node with HTML content, render it directly
	if instance.CurrentNodeDef != nil && instance.CurrentNodeDef.Type == "end" && instance.CurrentNodeDef.End != nil && instance.CurrentNodeDef.End.HTML != "" {
		tmpl, err := template.New("endNode").Parse(instance.CurrentNodeDef.End.HTML)
		if err != nil {
			log.Printf("Error parsing end node HTML template for instance %s: %v", instanceID, err)
			http.Error(w, "Failed to render end page due to template error.", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		// Execute the template, passing the workflow instance's context for data interpolation
		if execErr := tmpl.Execute(w, instance.Context); execErr != nil {
			log.Printf("Error executing end node HTML template for instance %s: %v", instanceID, execErr)
			http.Error(w, "Failed to render end page.", http.StatusInternalServerError)
		}
		return
	}

	// For other node types, return JSON status
	response := APIResponse{
		InstanceID:    instance.ID,
		WorkflowID:    instance.WorkflowID,
		CurrentNode:   instance.CurrentNode,
		Context:       instance.Context,
		WaitingSignal: instance.WaitingSignal,
		ExpiresAt:     instance.ExpiresAt,
		Message:       "Workflow instance status retrieved successfully.",
	}

	// If the current node is a form, include the form URL
	if instance.CurrentNodeDef != nil && instance.CurrentNodeDef.Type == "form" {
		response.FormURL = fmt.Sprintf("/form/%s", instance.ID)
	}

	sendJSONResponse(w, http.StatusOK, response)
}

// submitFormHandler handles requests to get form definitions or submit form data.
func submitFormHandler(w http.ResponseWriter, r *http.Request) {
	instanceID := filepath.Base(r.URL.Path)
	if instanceID == "form" || instanceID == "" {
		sendJSONResponse(w, http.StatusBadRequest, APIResponse{
			Error:   "Instance ID not provided. Usage: /form/{instanceID}",
			Message: "Missing instance ID.",
		})
		return
	}

	instance, err := workflow.GetInstanceAndDefinition(instanceID)
	if err != nil {
		if err == sql.ErrNoRows {
			sendJSONResponse(w, http.StatusNotFound, APIResponse{
				Error:   fmt.Sprintf("Workflow instance '%s' not found.", instanceID),
				Message: "Instance not found.",
			})
		} else {
			log.Printf("Error getting workflow instance for form %s: %v", instanceID, err)
			sendJSONResponse(w, http.StatusInternalServerError, APIResponse{
					Error:   fmt.Sprintf("Failed to retrieve form: %v", err),
					Message: "Internal server error.",
			})
		}
		return
	}

	// This check is CRUCIAL: Ensure it's a form node AND the Fields slice exists
	if instance.CurrentNodeDef == nil || instance.CurrentNodeDef.Type != "form" || instance.CurrentNodeDef.Fields == nil {
		sendJSONResponse(w, http.StatusBadRequest, APIResponse{
			Error:   fmt.Sprintf("Node '%s' for instance '%s' is not a valid form node or is missing form definition.", instance.CurrentNode, instanceID),
			Message: "Current node is not a form or form definition is incomplete.",
		})
		return
	}

	if r.Method == http.MethodGet {
		// On GET, GENERATE AND RENDER THE HTML FORM
		htmlForm, err := workflow.GenerateHTMLForm(instance.CurrentNodeDef.Fields, instance.Context, instance.ID, nil) // Pass nil for initial errors
		if err != nil {
			log.Printf("Error generating HTML form for instance %s: %v", instance.ID, err)
			http.Error(w, "Failed to render form due to internal error.", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlForm)) // Write the generated HTML to the response writer
		return
	}

	if r.Method == http.MethodPost {
		// On POST, PROCESS THE SUBMITTED FORM DATA
		log.Printf("Received form submission for instance %s", instanceID)

		// Parse form data from request body (form-urlencoded, typical for HTML forms)
		if err := r.ParseForm(); err != nil {
			log.Printf("Error parsing form data for instance %s: %v", instanceID, err)
			http.Error(w, "Failed to parse form submission.", http.StatusBadRequest)
			return
		}

		// Convert r.Form (map[string][]string) to map[string]string for validation/merge
		formDataStr := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formDataStr[key] = values[0] // Take the first value for each field
			}
		}

		// Validate the form input against the defined fields
		validationErrors := workflow.ValidateFormInput(instance.CurrentNodeDef.Fields, formDataStr)
		if len(validationErrors) > 0 {
			log.Printf("Form validation failed for instance %s: %v", instanceID, validationErrors)
			// If validation fails, re-render the form, passing the validation errors
			htmlFormWithErrors, err := workflow.GenerateHTMLForm(instance.CurrentNodeDef.Fields, instance.Context, instance.ID, validationErrors)
			if err != nil {
				log.Printf("Error regenerating HTML form with errors for instance %s: %v", instance.ID, err)
				http.Error(w, "Failed to re-render form with validation errors.", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest) // Use 400 Bad Request for validation errors
			w.Write([]byte(htmlFormWithErrors))
			return
		}

		// Convert formDataStr (map[string]string) to map[string]interface{} for AdvanceInstanceAfterForm
		formDataMap := make(map[string]interface{})
		for k, v := range formDataStr {
			formDataMap[k] = v // string can be assigned to interface{}
		}

// Merge validated form input into the workflow instance's context
// This line remains as it updates the local in-memory context before the DB save
workflow.MergeFormInputIntoContext(instance.Context, instance.CurrentNodeDef.Fields, formDataStr)

// Advance the workflow instance to the next node after the form
// The 'instance.Context' argument has been removed, as the function
// now retrieves and updates the context directly from the database.
err = workflow.AdvanceInstanceAfterForm(instance.ID, instance.CurrentNodeDef.Next, formDataMap)

if err != nil {
    log.Printf("Error advancing workflow after form submission for instance %s: %v", instanceID, err)
    http.Error(w, fmt.Sprintf("Failed to advance workflow after form: %v", err), http.StatusInternalServerError)
    return
}

		// On successful submission, redirect the user to the instance's status page
		http.Redirect(w, r, fmt.Sprintf("/status/%s", instance.ID), http.StatusFound)
		log.Printf("Form submitted and workflow advanced for instance %s", instanceID)
		return
	}

	sendJSONResponse(w, http.StatusMethodNotAllowed, APIResponse{
		Error:   "Method not allowed. Use GET or POST.",
		Message: "Invalid HTTP method for form endpoint.",
	})
}