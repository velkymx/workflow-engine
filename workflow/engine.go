package workflow

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"jbpmn-engine/db"
	"jbpmn-engine/scripts"

	"github.com/google/uuid"
)

var (
	workflowDefinitions     map[string]*Workflow
	workflowDefinitionsLock sync.RWMutex
	workflowDir             string
)

func SetWorkflowDirectory(dir string) {
	workflowDir = dir
	log.Printf("Workflow definitions will be primarily loaded from: %s", workflowDir)
}

func LoadWorkflowsFromDir(dir string) error {
	workflowDefinitionsLock.Lock()
	defer workflowDefinitionsLock.Unlock()

	workflowDefinitions = make(map[string]*Workflow)

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read workflow directory %s: %w", dir, err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: Failed to read workflow file %s: %v", filePath, err)
			continue
		}

		var wf Workflow
		err = json.Unmarshal(data, &wf)
		if err != nil {
			log.Printf("Warning: Failed to unmarshal workflow JSON from %s: %v", filePath, err)
			continue
		}

		workflowDefinitions[wf.ID] = &wf
		log.Printf("Loaded workflow definition: %s (ID: %s)", wf.Name, wf.ID)
	}
	return nil
}

func GetWorkflowDefinition(workflowID string) (*Workflow, error) {
	workflowDefinitionsLock.RLock()
	wf, ok := workflowDefinitions[workflowID]
	workflowDefinitionsLock.RUnlock()

	if !ok {
		if workflowDir == "" {
			return nil, fmt.Errorf("workflow directory not set. Call SetWorkflowDirectory first in main.")
		}

		filePath := filepath.Join(workflowDir, fmt.Sprintf("%s.json", workflowID))
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("workflow definition '%s' not found in memory and failed to read from file '%s': %w", workflowID, filePath, err)
		}

		var newWf Workflow
		err = json.Unmarshal(data, &newWf)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling workflow JSON from %s: %w", filePath, err)
		}

		workflowDefinitionsLock.Lock()
		defer workflowDefinitionsLock.Unlock()
		workflowDefinitions[newWf.ID] = &newWf
		log.Printf("Dynamically loaded workflow definition: %s (ID: %s) from disk.", newWf.Name, newWf.ID)

		_, _, _, existingRawJSON, _ := db.GetWorkflow(newWf.ID)
		if existingRawJSON != string(data) {
			log.Printf("Saving/Updating dynamically loaded workflow '%s' in DB.", newWf.ID)
			metaJSON, _ := json.Marshal(newWf.Meta)
			saveErr := db.SaveWorkflow(newWf.ID, newWf.Name, string(metaJSON), string(data))
			if saveErr != nil {
				log.Printf("Warning: Could not save dynamically loaded workflow %s to DB: %v", newWf.ID, saveErr)
			}
		}

		return &newWf, nil
	}
	return wf, nil
}

// CreateNewInstance creates a new workflow instance and its initial node execution record.
func CreateNewInstance(workflowID string) (*WorkflowInstance, error) {
	wf, err := GetWorkflowDefinition(workflowID)
	if err != nil {
		return nil, fmt.Errorf("workflow definition not found or invalid for ID %s: %v", workflowID, err)
	}

	instanceID := uuid.New().String()
	initialContext := make(map[string]interface{})
	initialContext["instanceID"] = instanceID

	startNode := wf.GetNodeByID("start_node")
	if startNode == nil {
		return nil, fmt.Errorf("workflow %s does not have a 'start_node'", workflowID)
	}

	waitingSignal := ""
	if startNode.Signal != nil && startNode.Signal.Catch != "" {
		waitingSignal = startNode.Signal.Catch
		log.Printf("Instance %s created for workflow %s. It is waiting for signal '%s' to start execution.", instanceID, workflowID, waitingSignal)
	} else {
		log.Printf("Instance %s created for workflow %s. Starting auto-execution.", instanceID, workflowID)
	}

	ctxJSON, err := json.Marshal(initialContext)
	if err != nil {
		return nil, fmt.Errorf("error marshalling initial context: %v", err)
	}

	// Use the new db.SaveNewInstance which handles both instance and initial node entry
	_, initialNodeInstanceDBID, err := db.SaveNewInstance(instanceID, workflowID, startNode.ID, string(ctxJSON), waitingSignal, nil)
	if err != nil {
		return nil, fmt.Errorf("error saving new workflow instance and initial node to DB: %v", err)
	}

	instance := &WorkflowInstance{
		ID:                      instanceID,
		WorkflowID:              workflowID,
		CurrentNode:             startNode.ID, // Node definition ID
		CurrentNodeInstanceDBID: initialNodeInstanceDBID, // UUID from db.workflow_instance_nodes
		Context:                 initialContext,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
		WorkflowDef:             wf,
		CurrentNodeDef:          startNode,
		WaitingSignal:           waitingSignal,
	}

	if waitingSignal == "" {
		go func() {
			execErr := ExecuteNextNode(instance.ID)
			if execErr != nil {
				log.Printf("Error during initial workflow execution for instance %s: %v", instance.ID, execErr)
			}
		}()
	}

	return instance, nil
}

// ExecuteNextNode fetches the instance, determines the next node, and executes it.
func ExecuteNextNode(instanceID string) error {
	instance, loadErr := GetInstanceAndDefinition(instanceID)
	if loadErr != nil {
		return fmt.Errorf("failed to load instance %s for execution: %v", instanceID, loadErr)
	}

	if instance.WaitingSignal != "" || (instance.ExpiresAt != nil && instance.ExpiresAt.Before(time.Now())) {
		log.Printf("Instance %s is waiting for signal ('%s') or has expired. Not auto-executing.", instanceID, instance.WaitingSignal)
		return nil
	}

	log.Printf("Executing node %s (Type: %s) for instance %s", instance.CurrentNode, instance.CurrentNodeDef.Type, instance.ID)

	if instance.CurrentNodeDef.Timeout != nil {
		go func(instID string, timeoutCfg *TimeoutConfig, originalNodeID string, originalNodeInstanceDBID string) {
			duration, err := time.ParseDuration(timeoutCfg.Duration)
			if err != nil {
				log.Printf("Error parsing timeout duration '%s' for instance %s: %v", timeoutCfg.Duration, instID, err)
				return
			}
			time.Sleep(duration)

			currentInstance, err := GetInstanceAndDefinition(instID)
			if err != nil {
				log.Printf("Error re-fetching instance %s for timeout check: %v", instID, err)
				return
			}

			// Only transition on timeout if still on the same node *instance*
			if currentInstance.CurrentNodeInstanceDBID == originalNodeInstanceDBID {
				log.Printf("Instance %s timed out at node %s. Transitioning to %s.", instID, originalNodeID, timeoutCfg.Next)
				// Use advanceInstance to handle the state update and new node instance creation
				advErr := advanceInstance(instID, timeoutCfg.Next, nil)
				if advErr != nil {
					log.Printf("Error advancing instance %s after timeout transition: %v", instID, advErr)
					return
				}
				// The advanceInstance function will already trigger ExecuteNextNode
			}
		}(instanceID, instance.CurrentNodeDef.Timeout, instance.CurrentNode, instance.CurrentNodeInstanceDBID)
	}

	var execErr error
	switch instance.CurrentNodeDef.Type {
	case "start":
		if instance.CurrentNodeDef.Next != "" {
			execErr = advanceInstance(instanceID, instance.CurrentNodeDef.Next, nil)
		} else {
			return fmt.Errorf("start node %s has no 'next' transition defined", instance.CurrentNode)
		}
	case "form":
		log.Printf("Instance %s is at form node %s, waiting for user input.", instance.ID, instance.CurrentNode)
		return nil
	case "script":
		execErr = executeScriptNode(instance)
	case "gateway":
		nextNodeID, signalToThrow, gatewayErr := ResolveGatewayConditions(instance)
		if gatewayErr != nil {
			return fmt.Errorf("error processing gateway node %s for instance %s: %w", instance.CurrentNode, instance.ID, gatewayErr)
		}

		if signalToThrow != "" {
			log.Printf("Engine emitting signal '%s' from gateway %s for instance %s", signalToThrow, instance.CurrentNode, instance.ID)
			go func() {
				emitErr := EmitSignal(signalToThrow)
				if emitErr != nil {
					log.Printf("Error emitting signal '%s' from gateway %s for instance %s: %v", signalToThrow, instance.CurrentNode, instance.ID, emitErr)
				}
			}()
		}
		execErr = advanceInstance(instance.ID, nextNodeID, nil)

	case "end":
		execErr = executeEndNode(instance)
	default:
		return fmt.Errorf("unsupported node type: %s for node %s", instance.CurrentNodeDef.Type, instance.CurrentNode)
	}

	if execErr != nil {
		log.Printf("Error executing node %s for instance %s: %v", instance.CurrentNode, instance.ID, execErr)
		return execErr
	}

	return nil
}

// advanceInstance updates the instance to the next node and saves a new node execution record.
func advanceInstance(instanceID, nextNodeID string, waitingSignal *string) error {
	instance, err := GetInstanceAndDefinition(instanceID)
	if err != nil {
		return fmt.Errorf("failed to load instance %s to advance: %v", instanceID, err)
	}

	instance.CurrentNode = nextNodeID // Update in memory for immediate use
	instance.CurrentNodeDef = instance.WorkflowDef.GetNodeByID(nextNodeID)
	instance.UpdatedAt = time.Now()

	signalString := ""
	if waitingSignal != nil {
		signalString = *waitingSignal
		instance.WaitingSignal = *waitingSignal // Update in memory
	} else {
		instance.WaitingSignal = "" // Clear in memory
	}

	ctxJSON, err := json.Marshal(instance.Context)
	if err != nil {
		return fmt.Errorf("error marshalling context for instance %s: %v", instance.ID, err)
	}

	// Use db.UpdateInstanceCurrentNodeAndContext to create a new node entry and update the main instance
	newNodeInstanceDBID, err := db.UpdateInstanceCurrentNodeAndContext(instance.ID, nextNodeID, string(ctxJSON), signalString, instance.ExpiresAt)
	if err != nil {
		return fmt.Errorf("error saving instance %s after advancing to %s: %v", instance.ID, nextNodeID, err)
	}
	instance.CurrentNodeInstanceDBID = newNodeInstanceDBID // Update in memory with the new DB ID

	if instance.CurrentNodeDef.Type != "end" && instance.CurrentNodeDef.Type != "form" && (waitingSignal == nil || *waitingSignal == "") {
		go func() {
			execErr := ExecuteNextNode(instanceID)
			if execErr != nil {
				log.Printf("Error executing next node %s for instance %s: %v", nextNodeID, instanceID, execErr)
			}
		}()
	}

	return nil
}

// AdvanceInstanceAfterForm updates an instance's context and moves it to the next node.
// This is specifically for advancing after a form submission.
func AdvanceInstanceAfterForm(instanceID, nextNodeID string, formData map[string]interface{}) error {
	instance, err := GetInstanceAndDefinition(instanceID)
	if err != nil {
		return fmt.Errorf("failed to load instance %s to advance after form: %w", instanceID, err)
	}

	if instance.Context == nil {
		instance.Context = make(map[string]interface{})
	}
	for key, value := range formData {
		instance.Context[key] = value
	}

	// Clear any waiting signal, as the form has been submitted
	instance.WaitingSignal = ""
	instance.ExpiresAt = nil

	ctxJSON, err := json.Marshal(instance.Context)
	if err != nil {
		return fmt.Errorf("error marshalling context after form submission for instance %s: %w", instanceID, err)
	}

	// Use db.UpdateInstanceCurrentNodeAndContext to create a new node entry and update the main instance
	newNodeInstanceDBID, err := db.UpdateInstanceCurrentNodeAndContext(instance.ID, nextNodeID, string(ctxJSON), "", nil)
	if err != nil {
		return fmt.Errorf("error saving instance %s after form submission: %w", instanceID, err)
	}
	instance.CurrentNodeInstanceDBID = newNodeInstanceDBID // Update in memory

	log.Printf("Instance %s advanced to node %s after form submission.", instanceID, nextNodeID)

	go func() {
		execErr := ExecuteNextNode(instanceID)
		if execErr != nil {
			log.Printf("Error executing node after form submission for instance %s: %v", instanceID, execErr)
		}
	}()

	return nil
}

func executeScriptNode(instance *WorkflowInstance) error {
	scriptConfig := instance.CurrentNodeDef.Script
	if scriptConfig == nil {
		return fmt.Errorf("script configuration missing for node %s", instance.CurrentNode)
	}

	newContext, err := scripts.ExecuteScript(scriptConfig.Code, instance.Context)
	if err != nil {
		return fmt.Errorf("error executing script for node %s: %v", instance.CurrentNode, err)
	}

	instance.Context = newContext

	return advanceInstance(instance.ID, instance.CurrentNodeDef.Next, nil)
}

func executeEndNode(instance *WorkflowInstance) error {
	log.Printf("Workflow instance %s ended at node %s.", instance.ID, instance.CurrentNode)

	endConfig := instance.CurrentNodeDef.End
	if endConfig != nil && endConfig.Signal != nil && endConfig.Signal.Emit != "" {
		log.Printf("End node %s for instance %s emitting signal: %s", instance.CurrentNode, instance.ID, endConfig.Signal.Emit)
		go func() {
			emitErr := EmitSignal(endConfig.Signal.Emit)
			if emitErr != nil {
				log.Printf("Error emitting signal '%s' from end node %s for instance %s: %v", endConfig.Signal.Emit, instance.CurrentNode, instance.ID, emitErr)
			}
		}()
	}

	return nil
}

// GetInstanceAndDefinition loads a workflow instance and its associated definition,
// retrieving the current node's definition from the new workflow_instance_nodes table.
func GetInstanceAndDefinition(instanceID string) (*WorkflowInstance, error) {
	// First, get the main instance record to find the current_node_instance_id
	id, workflowID, currentNodeInstanceDBID, contextStr, waitingSignal, expiresAt, createdAt, updatedAt, err := db.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("error getting instance %s from DB: %v", instanceID, err)
	}

	// Then, get the specific node instance details using currentNodeInstanceDBID
	_, _, currentNodeDefinitionID, _, _, _, _, _, err := db.GetNodeInstance(currentNodeInstanceDBID)
	if err != nil {
		return nil, fmt.Errorf("error getting current node instance details for instance %s (node instance %s): %v", instanceID, currentNodeInstanceDBID, err)
	}

	// Load the overall workflow definition
	wf, err := GetWorkflowDefinition(workflowID)
	if err != nil {
		return nil, fmt.Errorf("error getting workflow definition for instance %s (workflow %s): %v", instanceID, workflowID, err)
	}

	var ctx map[string]interface{}
	if contextStr != "" {
		err = json.Unmarshal([]byte(contextStr), &ctx)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling context for instance %s: %v", instanceID, err)
		}
	} else {
		ctx = make(map[string]interface{})
	}

	instance := &WorkflowInstance{
		ID:                      id,
		WorkflowID:              workflowID,
		CurrentNode:             currentNodeDefinitionID, // This is the node definition ID
		CurrentNodeInstanceDBID: currentNodeInstanceDBID, // This is the UUID from workflow_instance_nodes
		Context:                 ctx,
		WaitingSignal:           waitingSignal,
		ExpiresAt:               expiresAt,
		CreatedAt:               createdAt,
		UpdatedAt:               updatedAt,
		WorkflowDef:             wf,
		CurrentNodeDef:          wf.GetNodeByID(currentNodeDefinitionID),
	}

	if instance.CurrentNodeDef == nil {
		return nil, fmt.Errorf("current node definition '%s' not found in workflow definition for instance %s", currentNodeDefinitionID, instanceID)
	}

	return instance, nil
}

// GetNodeByID is a helper method on Workflow to find a node by its ID.
func (wf *Workflow) GetNodeByID(nodeID string) *WorkflowNode {
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == nodeID {
			nodeCopy := wf.Nodes[i]
			return &nodeCopy
		}
	}
	return nil
}