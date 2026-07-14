package llm

// Agent represents an AI agent with tools
type Agent struct {
	Name        string
	System      string
	Tools       []Tool
	ToolHandler func(ToolCall) (string, error)
	Client      *Client
}

// NewAgent creates a new agent
func NewAgent(name, systemPrompt string, tools []Tool, handler func(ToolCall) (string, error)) *Agent {
	return &Agent{
		Name:        name,
		System:      systemPrompt,
		Tools:       tools,
		ToolHandler: handler,
		Client:      NewClient(),
	}
}

// Run executes the agent with a user prompt
func (a *Agent) Run(userPrompt string) (string, error) {
	messages := []Message{
		NewMessage("system", a.System),
		NewMessage("user", userPrompt),
	}

	return a.Client.GetResponseWithTools(messages, a.Tools, a.ToolHandler)
}

// RunWithContext executes the agent with conversation context
func (a *Agent) RunWithContext(messages []Message) (string, error) {
	return a.Client.GetResponseWithTools(messages, a.Tools, a.ToolHandler)
}
