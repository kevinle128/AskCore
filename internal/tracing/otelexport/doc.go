// Package otelexport configures the OpenTelemetry tracer provider and the OTLP exporter.
//
// Set OTEL_SERVICE_NAME and OTEL_EXPORTER_OTLP_ENDPOINT (defaults to
// http://localhost:4318). SigNoz Cloud also requires OTEL_EXPORTER_OTLP_HEADERS
// with a signoz-ingestion-key. Call InitTracing early in main:
//
//	shutdown, err := otelexport.InitTracing(context.Background())
//	if err != nil { ... }
//	defer shutdown(context.Background())
//
// See README.md in this folder for what belongs here and the import rules.
package otelexport
