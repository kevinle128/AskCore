package providers

// Model is the minimal identity of one model of a provider. The model catalog
// extends this record with prices, compat data and more capabilities.
type Model struct {
	ID       string
	Name     string
	API      string
	Provider string
	// Reasoning reports that the model can produce thinking blocks.
	Reasoning bool
	// Input lists the input kinds the model accepts, for example "text" and "image".
	Input         []string
	ContextWindow int
	MaxTokens     int
}
