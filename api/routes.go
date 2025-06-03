package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"jbpmn-engine/db"
	"jbpmn-engine/workflow"

	"github.com/gorilla/mux"
)

// ConfigureRoutes sets up all the HTTP API endpoints.
func ConfigureRoutes(r *mux.Router) {
	r.HandleFunc("/start/{workflow_id}", StartWorkflowHandler).Methods("GET")
	r.HandleFunc("/signal/{signal_name}", SignalHandler).Methods("POST")
	r.HandleFunc("/form/{instance_id}", GetFormHandler).Methods("GET")
	r.HandleFunc("/form/{instance_id}", PostFormHandler).Methods("POST")
	r.HandleFunc("/status/{instance_id}", GetStatusHandler).Methods("GET")

	// Basic landing page or instructions
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<h1>JBPMN Workflow Engine</h1>
			<p>Available Routes:</p>
			<ul>
				<li><code>GET /start/:workflow_id</code> - Start a new workflow instance</li>
				<li><code>POST /signal/:signal_name</code> - Resume workflows waiting on a signal</li>
				<li><code>GET /form/:instance_id</code> - Render a form for a workflow instance</li>
				<li><code>POST /form/:instance_id</code> - Submit form data for a workflow instance</li>
				<li><code>GET /status/:instance_id</code> - Get current status and context of a workflow instance</li>
			</ul>
			<p>Ensure your workflow JSON files are in the <code>./workflows/</code> directory.</p>
		`)
	}).Methods("GET")
}

// StartWorkflowHandler handles the /start/:workflow_id route.
func StartWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["workflow_id"]

	instance, err := workflow.CreateNewInstance(workflowID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error starting workflow: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":      "Workflow instance created.",
		"instance_id":  instance.ID,
		"workflow_id":  instance.WorkflowID,
		"current_node": instance.CurrentNode,
		"status_url":   fmt.Sprintf("/status/%s", instance.ID),
		"form_url":     fmt.Sprintf("/form/%s", instance.ID), // Suggest form if applicable
	})
	log.Printf("API: Started workflow instance %s for workflow %s", instance.ID, workflowID)
}

// SignalHandler handles the /signal/:signal_name route.
func SignalHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	signalName := vars["signal_name"]

	err := workflow.ResumeWorkflowsBySignal(signalName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error processing signal: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Signal '%s' processed. Attempting to resume workflows.", signalName),
	})
	log.Printf("API: Received signal '%s'.", signalName)
}

// GetFormHandler renders an HTML form for a given instance.
func GetFormHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	instance, err := workflow.GetInstanceAndDefinition(instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow instance not found or error loading: %v", err), http.StatusNotFound)
		return
	}

	if instance.CurrentNodeDef.Type != "form" {
		http.Error(w, fmt.Sprintf("Instance %s is not currently at a form node. Current node type: %s", instanceID, instance.CurrentNodeDef.Type), http.StatusBadRequest)
		return
	}

	formConfig := instance.CurrentNodeDef.Form
	if formConfig == nil {
		http.Error(w, "Form configuration missing for current node.", http.StatusInternalServerError)
		return
	}

	// For GET requests, we don't have submission errors yet.
	formHTML, err := workflow.GenerateHTMLForm(formConfig, instance.Context, instanceID, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error generating form: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Workflow Form</title></head><body>
		<h1>Form for Instance: %s</h1>
		%s
	</body></html>`, instanceID, formHTML)
	log.Printf("API: Rendered form for instance %s at node %s", instanceID, instance.CurrentNode)
}

// PostFormHandler accepts form submission and processes it.
func PostFormHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	instance, err := workflow.GetInstanceAndDefinition(instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow instance not found or error loading: %v", err), http.StatusNotFound)
		return
	}

	if instance.CurrentNodeDef.Type != "form" {
		http.Error(w, fmt.Sprintf("Instance %s is not currently at a form node. Current node type: %s", instanceID, instance.CurrentNodeDef.Type), http.StatusBadRequest)
		return
	}

	formConfig := instance.CurrentNodeDef.Form
	if formConfig == nil {
		http.Error(w, "Form configuration missing for current node.", http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("Error parsing form data: %v", err), http.StatusBadRequest)
		return
	}

	inputData := make(map[string]string)
	for key, values := range r.PostForm {
		if len(values) > 0 {
			inputData[key] = values[0] // Take the first value for each field
		}
	}

	errors := workflow.ValidateFormInput(formConfig, inputData)
	if len(errors) > 0 {
		// If there are validation errors, re-render the form with errors
		formHTML, genErr := workflow.GenerateHTMLForm(formConfig, instance.Context, instanceID, errors)
		if genErr != nil {
			http.Error(w, fmt.Sprintf("Error regenerating form with errors: %v", genErr), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Workflow Form - Errors</title></head><body>
			<h1>Form for Instance: %s - Please correct errors</h1>
			%s
		</body></html>`, instanceID, formHTML)
		log.Printf("API: Form submission for instance %s had validation errors.", instanceID)
		return
	}

	// Merge validated input into workflow context
	workflow.MergeFormInputIntoContext(instance.Context, formConfig, inputData)

	// Save the updated context
	ctxJSON, err := json.Marshal(instance.Context)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshalling context: %v", err), http.StatusInternalServerError)
		return
	}
	// Update instance's current node to its 'next' for automatic progression
	nextNodeID := instance.CurrentNodeDef.Next
	if nextNodeID == "" {
		http.Error(w, fmt.Sprintf("Form node %s in instance %s does not define a 'next' node.", instance.CurrentNode, instanceID), http.StatusInternalServerError)
		return
	}

	// Update the instance state in DB to the next node, clearing expiration and waiting signal
	err = db.SaveInstance(instance.ID, instance.WorkflowID, nextNodeID, string(ctxJSON), "", nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error saving instance after form submission: %v", err), http.StatusInternalServerError)
		return
	}

	// Now execute the next node
	log.Printf("API: Form for instance %s submitted successfully. Moving to next node %s.", instanceID, nextNodeID)
	go func() {
		execErr := workflow.ExecuteNextNode(instanceID)
		if execErr != nil {
			log.Printf("Error executing workflow for instance %s after form submission: %v", instanceID, execErr)
			// Consider a mechanism to surface this error back to the user or admin
		}
	}()

	// Redirect to the next form or a status page
	nextInstance, _ := workflow.GetInstanceAndDefinition(instanceID) // Re-fetch to get new current node
	if nextInstance != nil && nextInstance.CurrentNodeDef != nil && nextInstance.CurrentNodeDef.Type == "form" {
		http.Redirect(w, r, fmt.Sprintf("/form/%s", instanceID), http.StatusSeeOther)
	} else if nextInstance != nil && nextInstance.CurrentNodeDef != nil && nextInstance.CurrentNodeDef.Type == "end" && nextInstance.CurrentNodeDef.End != nil && nextInstance.CurrentNodeDef.End.HTML != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, nextInstance.CurrentNodeDef.End.HTML) // Render final HTML if provided
	} else {
		// Default: redirect to status page
		http.Redirect(w, r, fmt.Sprintf("/status/%s", instanceID), http.StatusSeeOther)
	}
}

// GetStatusHandler returns the current state and context of a workflow instance.
func GetStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	instance, err := workflow.GetInstanceAndDefinition(instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Workflow instance not found or error loading: %v", err), http.StatusNotFound)
		return
	}

	// Prepare response data
	statusResponse := struct {
		InstanceID      string                 `json:"instance_id"`
		WorkflowID      string                 `json:"workflow_id"`
		CurrentNode     string                 `json:"current_node"`
		CurrentNodeType string                 `json:"current_node_type"`
		Context         map[string]interface{} `json:"context"`
		WaitingSignal   string                 `json:"waiting_signal,omitempty"`
		ExpiresAt       *string                `json:"expires_at,omitempty"`
		CreatedAt       string                 `json:"created_at"`
		UpdatedAt       string                 `json:"updated_at"`
		EndHTML         *string                `json:"end_html,omitempty"`
	}{
		InstanceID:      instance.ID,
		WorkflowID:      instance.WorkflowID,
		CurrentNode:     instance.CurrentNode,
		CurrentNodeType: instance.CurrentNodeDef.Type,
		Context:         instance.Context,
		WaitingSignal:   instance.WaitingSignal,
		CreatedAt:       instance.CreatedAt.Format(db.TimeFormat),
		UpdatedAt:       instance.UpdatedAt.Format(db.TimeFormat),
	}

	if instance.ExpiresAt != nil {
		expiresAtStr := instance.ExpiresAt.Format(db.TimeFormat)
		statusResponse.ExpiresAt = &expiresAtStr
	}

	if instance.CurrentNodeDef.Type == "end" && instance.CurrentNodeDef.End != nil && instance.CurrentNodeDef.End.HTML != "" {
		statusResponse.EndHTML = &instance.CurrentNodeDef.End.HTML
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusResponse)
	log.Printf("API: Retrieved status for instance %s.", instanceID)
}
