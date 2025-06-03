package workflow

import (
	"encoding/json"
	"fmt"
	"log"

	"jbpmn-engine/db"
)

// EmitSignal processes a signal, resuming any workflows waiting for it.
// In a real-world scenario, this might be triggered by a message queue or another service.
func EmitSignal(signalName string) error {
	log.Printf("Signal Emitted: %s. Attempting to resume waiting workflows...", signalName)
	// This function directly calls ResumeWorkflowsBySignal, which is also in this file.
	return ResumeWorkflowsBySignal(signalName)
}

// ResumeWorkflowsBySignal finds and resumes instances waiting for a specific signal.
func ResumeWorkflowsBySignal(signalName string) error {
	log.Printf("Attempting to resume workflows waiting for signal: %s", signalName)
	instanceIDs, err := db.GetInstancesWaitingForSignal(signalName)
	if err != nil {
		return fmt.Errorf("error getting instances waiting for signal %s: %w", signalName, err)
	}

	if len(instanceIDs) == 0 {
		log.Printf("No instances found waiting for signal: %s", signalName)
		return nil
	}

	for _, id := range instanceIDs {
		// GetInstanceAndDefinition is in engine.go, but callable directly as it's in the same package.
		instance, err := GetInstanceAndDefinition(id)
		if err != nil {
			log.Printf("Error loading instance %s to resume by signal %s: %v", id, signalName, err)
			continue
		}

		// Prepare context for saving (no changes to context itself, but it's part of the save payload)
		ctxJSON, err := json.Marshal(instance.Context)
		if err != nil {
			log.Printf("Error marshalling context for instance %s before resuming: %v", id, err)
			continue
		}

		// Update the instance: clear the waiting signal and save a new node instance record.
		// The node ID remains the same, but a new entry in workflow_instance_nodes marks the signal reception.
		_, err = db.UpdateInstanceCurrentNodeAndContext(
			instance.ID,
			instance.CurrentNode, // The current node definition ID remains the same
			string(ctxJSON),
			"", // Clear waiting signal
			instance.ExpiresAt,
		)
		if err != nil {
			log.Printf("Error updating instance %s after clearing signal: %v", id, err)
			continue
		}

		log.Printf("Resuming instance %s which was waiting for signal '%s'.", id, signalName)
		go func(instanceIDToResume string) {
			execErr := ExecuteNextNode(instanceIDToResume) // Execute the node where it left off
			if execErr != nil {
				log.Printf("Error executing node for instance %s after signal %s: %v", instanceIDToResume, signalName, execErr)
			}
		}(id)
	}
	return nil
}