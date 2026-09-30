package gateway

// Config holds the network settings of the servers. internal/app fills it
// from config.Config.
type Config struct {
	Host       string
	Port       int
	GRPCPort   int
	CORSOrigin string
}
