package tool

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type TodoTool struct {
}

func NewTodoTool() *TodoTool {
	return &TodoTool{}
}

type TodoToolParam struct {
	Subject     string `json:"subject"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	ActiveForm  string `json:"active_form,omitempty"`
}

func (t *TodoTool) ToolName() AgentTool {
	return AgentToolTodo
}

func (t *TodoTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolTodo),
		Description: openai.String("Track multi-step tasks with status management.\n\nUse when:\n- Complex tasks requiring multiple steps\n- User explicitly asks to track progress\n- Planning phase for non-trivial work\n\nAvoid when:\n- Single-step tasks\n- Simple, quick requests\n\nStatus values: pending | in_progress | completed"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"subject": map[string]any{
					"type":        "string",
					"description": "task title (required, brief imperative form like 'Fix login bug')",
				},
				"status": map[string]any{
					"type":        "string",
					"description": "task status: pending | in_progress | completed",
					"enum":        []string{"pending", "in_progress", "completed"},
				},
				"description": map[string]any{
					"type":        "string",
					"description": "detailed task description (optional)",
				},
				"active_form": map[string]any{
					"type":        "string",
					"description": "description shown when status is in_progress (optional)",
				},
			},
			"required": []string{"subject", "status"},
		},
	})
}

func (t *TodoTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := TodoToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// Validation
	if p.Subject == "" {
		return "", &ValidationError{Field: "subject", Message: "subject is required"}
	}
	if p.Status != "pending" && p.Status != "in_progress" && p.Status != "completed" {
		return "", &ValidationError{Field: "status", Message: "status must be pending, in_progress, or completed"}
	}

	// In a full implementation, this would interact with a task storage system
	// For now, return a confirmation message
	statusText := map[string]string{
		"pending":     "marked as pending",
		"in_progress": "marked as in progress",
		"completed":   "marked as completed",
	}[p.Status]

	return "Task " + statusText + ": " + p.Subject, nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "Validation error: " + e.Message
}
