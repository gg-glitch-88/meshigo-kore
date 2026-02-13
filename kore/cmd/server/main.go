package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gg-glitch-88/meshigo-kore/internal/adapters"
	"github.com/gg-glitch-88/meshigo-kore/internal/domain"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// 1. Initialize Adapters (Infrastructure)
	repo := adapters.NewInMemoryRepo()

	// 2. Inject into Domain Logic
	// The compiler checks here if 'repo' satisfies 'domain.DataProvider'
	logic := domain.NewLogicHandler(repo)

	// 3. Execute
	ctx := context.Background()
	if err := logic.Execute(ctx, "user-123"); err != nil {
		logger.Error("Execution failed", "error", err)
		os.Exit(1)
	}

	logger.Info("Process finished successfully")
}