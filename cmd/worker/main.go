package main

import (
	"backend-challenge-go-jungle-gaming/internal/di"
	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		di.ConfigModule,
		di.DatabaseModule,
		di.UsecaseModule,
		di.MessagingModule,
	)

	app.Run()
}
