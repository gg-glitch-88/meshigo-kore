package domain

import "context"

// DataProvider is an interface this logic NEEDS to work.
// Notice we define it here, not in the database package.
type DataProvider interface {
	FetchData(ctx context.Context, id string) (string, error)
}

// LogicHandler holds the dependencies.
type LogicHandler struct {
	provider DataProvider // Dependency Injection
}

// NewLogicHandler is the constructor.
func NewLogicHandler(dp DataProvider) *LogicHandler {
	return &LogicHandler{
		provider: dp,
	}
}

// Execute is your core business logic.
func (l *LogicHandler) Execute(ctx context.Context, id string) error {
	// 1. Get Data
	data, err := l.provider.FetchData(ctx, id)
	if err != nil {
		return err
	}

	// 2. Process (Insert your expert logic here)
	// Go prefers explicit error handling over exceptions.
	if len(data) == 0 {
		return nil
	}

	return nil
}