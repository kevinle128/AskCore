// Package testsupport holds shared test fixtures and interfaces used by
// generated example tests.
package testsupport

//go:generate go run go.uber.org/mock/mockgen -destination=mocks/greeter_mock.go -package=mocks AskCore/internal/testsupport Greeter

// Greeter is an example interface for mock generation. Run
// `go generate ./internal/testsupport` to produce mocks with mockgen.
type Greeter interface {
	Greet(name string) string
}

// Hello is an example function exercised by the generated tests.
func Hello(name string) string {
	if name == "" {
		name = "world"
	}
	return "Hello, " + name + "!"
}
