package db

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"time"
)

var DB *sql.DB

const TimeFormat = time.RFC3339

func InitDB(dataSourceName string) error {
	var err error
	DB, err = sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}

	if err = DB.Ping(); err != nil {
		DB.Close()
		return fmt.Errorf("error connecting to database: %w", err)
	}

	createTablesSQL := `
    CREATE TABLE IF NOT EXISTS workflows (
        id TEXT PRIMARY KEY,
        name TEXT,
        meta TEXT,
        raw_json TEXT
    );

    CREATE TABLE IF NOT EXISTS workflow_instances (
        id TEXT PRIMARY KEY,
        workflow_id TEXT,        
        current_node_instance_id TEXT, 
        context TEXT,
        waiting_signal TEXT,
        expires_at DATETIME,
        created_at DATETIME,
        updated_at DATETIME
    );
    
    CREATE TABLE IF NOT EXISTS workflow_instance_nodes (
        id TEXT PRIMARY KEY,               -- UUID for this specific node instance
        workflow_instance_id TEXT NOT NULL, -- Foreign key to workflow_instances
        node_id TEXT NOT NULL,             -- The ID of the node definition (e.g., "start_node", "check_age_gateway")
        context TEXT,                      -- Context at the moment this node was entered/processed
        waiting_signal TEXT,               -- If the instance is waiting for a signal at THIS node
        expires_at DATETIME,               -- If this node has a timeout
        created_at DATETIME,
        updated_at DATETIME,
        -- Add any other relevant node-specific state here, e.g., 'status', 'output' etc.
        FOREIGN KEY (workflow_instance_id) REFERENCES workflow_instances(id)
    );
    `
	_, err = DB.Exec(createTablesSQL)
	if err != nil {
		DB.Close()
		return fmt.Errorf("error creating tables: %w", err)
	}
	log.Println("Database initialized and tables ensured.")
	return nil
}

func CloseDB() error {
	if DB != nil {
		err := DB.Close()
		if err != nil {
			log.Printf("Error closing database: %v", err)
			return fmt.Errorf("failed to close database: %w", err)
		}
		log.Println("Database connection closed.")
	}
	return nil
}

func SaveWorkflow(id, name, meta, rawJSON string) error {
	_, err := DB.Exec(
		"INSERT INTO workflows (id, name, meta, raw_json) VALUES (?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, meta=excluded.meta, raw_json=excluded.raw_json",
		id, name, meta, rawJSON,
	)
	return err
}

func GetWorkflow(id string) (id_ string, name, meta, rawJSON string, err error) {
	row := DB.QueryRow("SELECT id, name, meta, raw_json FROM workflows WHERE id = ?", id)
	err = row.Scan(&id_, &name, &meta, &rawJSON)
	return
}

// SaveNewInstance creates a new workflow instance and its initial node entry.
// It returns the ID of the new instance and the ID of the initial node instance.
func SaveNewInstance(instanceID, workflowID, initialNodeID, context, waitingSignal string, expiresAt *time.Time) (string, string, error) {
	now := time.Now()
	var expiresAtStr *string
	if expiresAt != nil {
		s := expiresAt.Format(TimeFormat)
		expiresAtStr = &s
	}

	// Insert into workflow_instances
	_, err := DB.Exec(
		`INSERT INTO workflow_instances (id, workflow_id, current_node_instance_id, context, waiting_signal, expires_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		instanceID, workflowID, "", context, waitingSignal, expiresAtStr, now.Format(TimeFormat), now.Format(TimeFormat),
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to save new workflow instance: %w", err)
	}

	// Create and save the initial workflow_instance_node entry
	initialNodeInstanceID := initialNodeID + "-" + instanceID // A simple unique ID for the initial node instance
	_, err = DB.Exec(
		`INSERT INTO workflow_instance_nodes (id, workflow_instance_id, node_id, context, waiting_signal, expires_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		initialNodeInstanceID, instanceID, initialNodeID, context, waitingSignal, expiresAtStr, now.Format(TimeFormat), now.Format(TimeFormat),
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to save initial workflow instance node: %w", err)
	}

	// Update the workflow_instances table with the actual current_node_instance_id
	_, err = DB.Exec(
		`UPDATE workflow_instances SET current_node_instance_id = ? WHERE id = ?`,
		initialNodeInstanceID, instanceID,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to update workflow instance with initial node instance ID: %w", err)
	}

	return instanceID, initialNodeInstanceID, nil
}

// UpdateInstanceCurrentNodeAndContext updates the main workflow instance record
// and creates a new entry in workflow_instance_nodes for the transition.
func UpdateInstanceCurrentNodeAndContext(instanceID, newNodeID string, newContext string, waitingSignal string, expiresAt *time.Time) (string, error) {
	now := time.Now()
	var expiresAtStr *string
	if expiresAt != nil {
		s := expiresAt.Format(TimeFormat)
		expiresAtStr = &s
	}

	// First, insert the new node entry into workflow_instance_nodes
	newNodeInstanceID := newNodeID + "-" + instanceID + "-" + fmt.Sprintf("%d", now.UnixNano()) // More unique ID
	_, err := DB.Exec(
		`INSERT INTO workflow_instance_nodes (id, workflow_instance_id, node_id, context, waiting_signal, expires_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		newNodeInstanceID, instanceID, newNodeID, newContext, waitingSignal, expiresAtStr, now.Format(TimeFormat), now.Format(TimeFormat),
	)
	if err != nil {
		return "", fmt.Errorf("failed to save new workflow instance node: %w", err)
	}

	// Then, update the main workflow_instances record's current_node_instance_id
	_, err = DB.Exec(
		`UPDATE workflow_instances SET
            current_node_instance_id = ?,
            context = ?,
            waiting_signal = ?,
            expires_at = ?,
            updated_at = ?
        WHERE id = ?`,
		newNodeInstanceID, newContext, waitingSignal, expiresAtStr, now.Format(TimeFormat), instanceID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to update workflow instance with new current node instance ID: %w", err)
	}

	return newNodeInstanceID, nil
}

// GetInstance retrieves a workflow instance by its ID.
// This now returns the current_node_instance_id instead of current_node (the definition ID).
func GetInstance(instanceID string) (id, workflowID, currentNodeInstanceID, context, waitingSignal string, expiresAt *time.Time, createdAt, updatedAt time.Time, err error) {
	var expiresAtStr, createdAtStr, updatedAtStr sql.NullString
	row := DB.QueryRow("SELECT id, workflow_id, current_node_instance_id, context, waiting_signal, expires_at, created_at, updated_at FROM workflow_instances WHERE id = ?", instanceID)
	err = row.Scan(&id, &workflowID, &currentNodeInstanceID, &context, &waitingSignal, &expiresAtStr, &createdAtStr, &updatedAtStr)

	if err == nil {
		if expiresAtStr.Valid {
			t, parseErr := time.Parse(TimeFormat, expiresAtStr.String)
			if parseErr == nil {
				expiresAt = &t
			}
		}
		if createdAtStr.Valid {
			createdAt, _ = time.Parse(TimeFormat, createdAtStr.String)
		}
		if updatedAtStr.Valid {
			updatedAt, _ = time.Parse(TimeFormat, updatedAtStr.String)
		}
	}
	return
}

// GetNodeInstance retrieves a specific workflow_instance_node by its ID.
func GetNodeInstance(nodeInstanceID string) (id, workflowInstanceID, nodeID, context, waitingSignal string, expiresAt *time.Time, createdAt, updatedAt time.Time, err error) {
	var expiresAtStr, createdAtStr, updatedAtStr sql.NullString
	row := DB.QueryRow("SELECT id, workflow_instance_id, node_id, context, waiting_signal, expires_at, created_at, updated_at FROM workflow_instance_nodes WHERE id = ?", nodeInstanceID)
	err = row.Scan(&id, &workflowInstanceID, &nodeID, &context, &waitingSignal, &expiresAtStr, &createdAtStr, &updatedAtStr)

	if err == nil {
		if expiresAtStr.Valid {
			t, parseErr := time.Parse(TimeFormat, expiresAtStr.String)
			if parseErr == nil {
				expiresAt = &t
			}
		}
		if createdAtStr.Valid {
			createdAt, _ = time.Parse(TimeFormat, createdAtStr.String)
		}
		if updatedAtStr.Valid {
			updatedAt, _ = time.Parse(TimeFormat, updatedAtStr.String)
		}
	}
	return
}

// GetInstancesWaitingForSignal retrieves instances waiting for a specific signal.
// This now queries the workflow_instances table directly for the main signal field.
func GetInstancesWaitingForSignal(signalName string) ([]string, error) {
	rows, err := DB.Query("SELECT id FROM workflow_instances WHERE waiting_signal = ?", signalName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instanceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		instanceIDs = append(instanceIDs, id)
	}
	return instanceIDs, nil
}

// GetExpiredInstances retrieves all workflow instances that have expired.
// This now queries the workflow_instances table directly for the main expires_at field.
func GetExpiredInstances() ([]string, error) {
	rows, err := DB.Query("SELECT id FROM workflow_instances WHERE expires_at IS NOT NULL AND expires_at <= ?", time.Now().Format(TimeFormat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instanceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		instanceIDs = append(instanceIDs, id)
	}
	return instanceIDs, nil
}