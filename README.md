# workflow-engine: A Go-based Workflow Engine

`workflow-engine` is a lightweight, opinionated workflow engine built in Go, designed to manage long-running business processes with persistent state. Inspired by BPMN concepts, it allows you to define workflows using JSON, execute them, and track their progress through various nodes. The engine supports common workflow patterns like sequential tasks, conditional branching (gateways), script execution, human interaction (forms), event-driven transitions (signals), and time-based progression (timeouts).

A core feature of this engine is its robust persistence layer, which uses SQLite to store the state of workflow definitions, running instances, and, critically, **individual node executions**, each with its own unique identifier (UUID) for comprehensive auditing and debugging.

## Features

  * **BPMN-Inspired Workflow Definitions**: Define your business processes using a clear, human-readable JSON structure.
  * **Persistent State**: All workflow definitions and instance states are stored in a SQLite database, ensuring durability across restarts.
  * **Detailed Node Instance Tracking**: Each time a workflow instance processes or transitions to a node, a new record with a unique UUID is stored in `workflow_instance_nodes`, providing a granular history of execution.
  * **Sequential Execution**: Define a linear flow of tasks.
  * **Conditional Gateways**: Implement branching logic based on dynamic context data.
  * **Script Execution**: Integrate custom Go-based scripts (or extend to other scripting languages) to manipulate workflow context.
  * **Form Handling**: Pause workflows for user input via defined forms and resume upon submission.
  * **Signal-driven Communication**: Advance workflows based on external events (signals).
  * **Timeouts**: Configure nodes to automatically transition after a specified duration.
  * **Extensible Design**: Built in Go, making it easy to extend and integrate into larger applications.

## Getting Started

These instructions will get you a copy of the project up and running on your local machine for development and testing purposes.

### Prerequisites

  * Go (version 1.18 or higher recommended)
  * SQLite3 (usually pre-installed on most systems, or easily installed via your package manager)

### Installation

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/velkymx/workflow-engine.git # Replace with actual repo URL
    cd workflow-engine
    ```
2.  **Initialize Go modules and download dependencies:**
    ```bash
    go mod tidy
    ```

### Running the Example

The `main.go` file provides a simple HTTP server demonstrating how to use the workflow engine.

1.  **Run the application:**

    ```bash
    go run main.go
    ```

    The server will start on `http://localhost:8080`. It will create a `workflow.db` file in the current directory if it doesn't exist.

2.  **Load Workflow Definitions**:
    The engine automatically loads workflow definitions from the `workflows/` directory. You can define your own workflows there (e.g., `simple_workflow.json`).

3.  **Interact with the API (using `curl` or a tool like Postman/Insomnia):**

      * **Create a new workflow instance:**

        ```bash
        curl -X POST http://localhost:8080/workflow/create -d '{"workflowId": "your_workflow_id"}' -H "Content-Type: application/json"
        ```

        (Replace `"your_workflow_id"` with the ID of a JSON workflow file you have, e.g., `"simple_workflow"` if you have `workflows/simple_workflow.json`).
        This will return the `instanceID` of the newly created workflow.

      * **Get a workflow instance's state:**

        ```bash
        curl http://localhost:8080/workflow/instance/{instanceID}
        ```

        (Replace `{instanceID}` with the actual ID returned from the create step).

      * **Emit a signal:**
        If your workflow is waiting for a signal, you can emit one:

        ```bash
        curl -X POST http://localhost:8080/signal/emit -d '{"signalName": "your_signal_name"}' -H "Content-Type: application/json"
        ```

        (Replace `"your_signal_name"` with the signal your workflow is waiting for).

      * **Submit a form:**
        If your workflow is at a "form" node, you can submit data:

        ```bash
        curl -X POST http://localhost:8080/form/submit -d '{"instanceId": "your_instance_id", "formData": {"field1": "value1", "field2": "value2"}}' -H "Content-Type: application/json"
        ```

## Core Concepts

### Workflows

A workflow is defined by a JSON file that outlines a series of connected `nodes`. Each workflow has a unique `ID` and a `Name`.

### Nodes

Nodes are the building blocks of a workflow. Each node has an `ID` and a `Type`, determining its behavior.

  * **`start`**: The entry point of a workflow. Must have a `next` transition.
  * **`end`**: The termination point of a workflow. Can optionally emit a signal.
  * **`script`**: Executes custom Go code (or other configured scripts) to manipulate the workflow's `Context`.
  * **`form`**: Pauses the workflow, typically waiting for user input. It defines `fields` for data collection.
  * **`gateway`**: Implements conditional branching. Based on `conditions` evaluating the `Context`, it directs the flow to a `next` node. Can also `throw` signals.
  * **Implicit Wait Nodes**: Any node can define a `signal.catch` to pause execution until that signal is received, or a `timeout` to automatically advance after a duration.

### Workflow Instances

A `WorkflowInstance` represents a single running execution of a `Workflow` definition. It maintains its current position (`CurrentNode`), its data (`Context`), and its status (e.g., `WaitingSignal`, `ExpiresAt`).

### Context

The `Context` is a `map[string]interface{}` that holds dynamic data as the workflow progresses. It's passed from node to node, allowing information gathered or processed at one step to be used in subsequent steps.

### Signals

Signals are a mechanism for asynchronous communication. A node can `catch` a signal to pause execution until it's `emit`ted, or it can `emit`/`throw` a signal to trigger other parts of the system or other workflows.

### Timeouts

Any node can define a `timeout` configuration. If the workflow instance remains at that node for longer than the specified `Duration`, it will automatically transition to the `Next` node defined in the timeout configuration.

### Persistent State (Database Schema)

The engine uses SQLite for state persistence. Key tables include:

  * `workflows`: Stores the JSON definitions of all deployed workflows.
  * `workflow_instances`: Holds the current state of active workflow instances, including their unique ID, associated workflow ID, current context, and the **ID of their current `workflow_instance_nodes` entry**.
  * `workflow_instance_nodes`: This critical table stores a unique record (with its own UUID) for *each time a workflow instance enters or transitions to a node*. This provides a complete chronological history of every step an instance has taken, including the context at that specific point, enabling powerful auditing and debugging.

## Workflow Definition Example (Simplified)

```json
{
  "id": "onboarding_process",
  "name": "New Employee Onboarding",
  "meta": {
    "description": "Workflow for onboarding new employees."
  },
  "nodes": [
    {
      "id": "start_node",
      "type": "start",
      "name": "Start Onboarding",
      "next": "collect_personal_info"
    },
    {
      "id": "collect_personal_info",
      "type": "form",
      "name": "Personal Information Form",
      "fields": [
        {"id": "full_name", "name": "Full Name", "type": "text", "required": true},
        {"id": "email", "name": "Email Address", "type": "email", "required": true}
      ],
      "next": "check_hr_approval"
    },
    {
      "id": "check_hr_approval",
      "type": "gateway",
      "name": "HR Approval Gateway",
      "conditions": [
        {
          "when": "context.hrApproved === true",
          "next": "setup_it_access"
        },
        {
          "else": true,
          "next": "send_rejection_email",
          "signal": {"throw": "onboarding_rejected"}
        }
      ]
    },
    {
      "id": "setup_it_access",
      "type": "script",
      "name": "Setup IT Access",
      "script": {
        "code": "console.log('Setting up IT access for ' + context.fullName);"
      },
      "next": "send_welcome_kit"
    },
    {
      "id": "send_welcome_kit",
      "type": "script",
      "name": "Send Welcome Kit",
      "script": {
        "code": "context.welcomeKitSent = true; console.log('Welcome kit sent!');"
      },
      "next": "end_onboarding"
    },
    {
      "id": "send_rejection_email",
      "type": "script",
      "name": "Send Rejection Email",
      "script": {
        "code": "console.log('Sending rejection email to ' + context.email);"
      },
      "next": "end_onboarding"
    },
    {
      "id": "end_onboarding",
      "type": "end",
      "name": "Onboarding Complete",
      "signal": {"emit": "employee_onboarded"}
    }
  ]
}
```

## Contributing

Contributions are welcome\! Please feel free to open issues, submit pull requests, or suggest new features.

## License

This project is licensed under the MIT License - see the [LICENSE](https://www.google.com/search?q=LICENSE) file for details.
