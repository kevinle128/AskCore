package app

import "go.uber.org/fx"

type Config struct {
	Name string
}

func NewConfig() Config { return Config{Name: "AskCore"} }

func New() *fx.App {
	return fx.New(fx.Provide(NewConfig))
}
