// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// buildResource describes the running service for every signal. It layers the
// service identity over resource.Default()'s SDK/host attributes; the
// service.* attributes are schemaless, so the merge cannot raise a schema-URL
// conflict. A non-nil error is a best-effort warning — the returned resource is
// still usable — so Init ignores it rather than failing.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(orDefault(cfg.ServiceName, "evolve")),
			semconv.ServiceVersion(orDefault(cfg.ServiceVersion, "dev")),
		),
	)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
