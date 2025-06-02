# JSON BPMN Execution Schema (v1.0)

This schema defines a compact, expressive format for executable workflows using BPMN concepts—adapted for real-time engines with signal-based communication, optional timer-based scheduling, and task-level timeouts.

---

## 🔷 Top-Level Workflow Object

```json
{
  "id": "workflow-id",
  "name": "Workflow Name",
  "meta": { },
  "nodes": [ ... ]
}
```

| Field   | Type   | Description                          |
| ------- | ------ | ------------------------------------ |
| `id`    | string | Unique workflow ID                   |
| `name`  | string | Human-readable name                  |
| `meta`  | object | Optional metadata for system use     |
| `nodes` | array  | List of task nodes (see types below) |

---

## 🧩 Node Types & Fields

### 🟢 `start`

Begins workflow execution. Can:

* Auto-start (via `next`)
* Wait for a **signal** (`signal.catch`)
* Be **timer-based** (e.g., every Monday at 8 AM)

```json
{
  "id": "start-1",
  "type": "start",
  "name": "Trigger on Signal or Timer",
  "signal": { "catch": "user:registered" },
  "timer": {
    "cron": "0 8 * * 1",
    "timezone": "America/Los_Angeles"
  },
  "next": "task-1"
}
```

| Field            | Type   | Description                       |
| ---------------- | ------ | --------------------------------- |
| `signal.catch`   | string | Waits for named signal to trigger |
| `timer.cron`     | string | CRON syntax to trigger start      |
| `timer.timezone` | string | Optional timezone string (IANA)   |
| `next`           | string | First task to run                 |

---

### 🔴 `end`

Marks the end of a workflow instance. Can **emit a signal** that triggers other workflows.

```json
{
  "id": "end-1",
  "type": "end",
  "name": "Finish",
  "html": "<h2>Thank you!</h2>",
  "signal": { "throw": "user:registered" }
}
```

| Field          | Type   | Description                           |
| -------------- | ------ | ------------------------------------- |
| `html`         | string | HTML to display at the end (optional) |
| `signal.throw` | string | Emits signal when completed           |

---

### 🧠 `script`

Executes secure JavaScript logic using `vm2`. The returned object updates the input context. To ensure safety, the script should be encoded in Base64 and decoded before execution.

```json
{
  "id": "script-1",
  "type": "script",
  "name": "Log Activity",
  "script": "cmV0dXJuIHsgbG9nZ2VkOiB0cnVlIH0=",
  "timeout": {
    "seconds": 60,
    "next": "task-overflow"
  },
  "next": "end-1"
}
```

| Field         | Type   | Description                                      |
| ------------- | ------ | ------------------------------------------------ |
| `script`      | string | Base64-encoded JavaScript to run in VM           |
| `timeout`     | object | Optional timeout behavior                        |
| → `seconds`   | number | Number of seconds before timeout triggers        |
| → `onTimeout` | string | Node to route to if timeout occurs               |
| `next`        | string | Next node to continue after successful execution |

---

### 📝 `form`

Displays a form and collects user input. Input is merged into the workflow context.

```json
{
  "id": "form-1",
  "type": "form",
  "name": "User Input",
  "fields": [
    { "name": "email", "type": "text", "label": "Email", "required": true },
    { "name": "age", "type": "number", "label": "Age" }
  ],
  "timeout": {
    "seconds": 1200,
    "next": "form-timeout"
  },
  "next": "gateway-1"
}
```

---

### 🔀 `gateway`

Branches logic based on evaluated conditions. The engine tests `condition[]` entries in order. You can optionally emit a signal using `signal.throw` inside a condition.

```json
{
  "id": "gateway-1",
  "type": "gateway",
  "name": "Check Role",
  "condition": [
    {
      "when": "process_data.role === 'admin'",
      "then": "admin-task",
      "signal": { "throw": "admin:entered" }
    },
    {
      "else": true,
      "then": "user-task"
    }
  ]
}
```

| Field            | Type    | Description                            |
| ---------------- | ------- | -------------------------------------- |
| `condition[]`    | array   | Ordered list of decision conditions    |
| → `when`         | string  | JS expression to evaluate (if present) |
| → `else`         | boolean | Optional fallback condition            |
| → `then`         | string  | Node ID to route to if rule matches    |
| → `signal.throw` | string  | Optional signal to emit on match       |

---

## 🧪 Example: Logic Gateway Based on Process Data

```json
{
  "id": "evaluate-loan",
  "type": "gateway",
  "name": "Check Credit Score",
  "condition": [
    {
      "when": "process_data.creditScore >= 700",
      "next": "task-approve"
    },
    {
      "when": "process_data.creditScore >= 600",
      "next": "task-review"
    },
    {
      "else": true,
      "next": "task-deny",
      "signal": { "throw": "loan:denied" }
    }
  ]
}
```

---

## 🔁 Signal Handling Summary

| Use Case                 | Supported At          | Field          |
| ------------------------ | --------------------- | -------------- |
| Start workflow on signal | `start`               | `signal.catch` |
| Emit signal on end       | `end`                 | `signal.throw` |
| Emit signal on gateway   | `gateway.condition[]` | `signal.throw` |

---

## 🕒 Timer Handling Summary

| Use Case                | Supported At     | Field                          |
| ----------------------- | ---------------- | ------------------------------ |
| Schedule workflow start | `start`          | `timer.cron` + `timezone`      |
| Timeout on script/form  | `script`, `form` | `timeout.seconds`, `onTimeout` |

---

## 🛠️ Example: Signal-Based Workflow Triggering Another

### Workflow A: Registration

```json
{
  "id": "register-user",
  "nodes": [
    { "id": "start-1", "type": "start", "next": "script-1" },
    { "id": "script-1", "type": "script", "script": "cmV0dXJuIHsgZW1haWw6IHByb2Nlc3NfZGF0YS5lbWFpbCB9", "next": "end-1" },
    { "id": "end-1", "type": "end", "signal": { "throw": "user:registered" } }
  ]
}
```

### Workflow B: Send Welcome Email

```json
{
  "id": "welcome-email",
  "nodes": [
    { "id": "start-1", "type": "start", "signal": { "catch": "user:registered" }, "next": "script-1" },
    { "id": "script-1", "type": "script", "script": "c2VuZEVtYWlsKHByb2Nlc3NfZGF0YS5lbWFpbCwgJ1dlbGNvbWUhJyk=", "next": "end-1" },
    { "id": "end-1", "type": "end" }
  ]
}
```

---

This schema enables compact, testable, reactive workflow design across forms, logic, timers, timeouts, and signals. Use it as the core execution format in your workflow engine.
