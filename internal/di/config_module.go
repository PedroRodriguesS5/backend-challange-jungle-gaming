package di

import (
	"log/slog"
	"os"

	"backend-challenge-go-jungle-gaming/internal/infra/config"
	"go.uber.org/fx"
)

var ConfigModule = fx.Module("config",
	fx.Provide(
		config.Load,
		provideLogger,
	),
)

func provideLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}
