// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// scopeName is this package's OpenTelemetry instrumentation scope. The engine
// reaches the global tracer through it rather than importing internal/telemetry,
// so telemetry can import internal/run for its reporter decorator without a
// cycle.
const scopeName = "github.com/bitwise-media-group/evolve/internal/run"

func tracer() trace.Tracer { return otel.Tracer(scopeName) }

// endSpan ends span, recording err as the span's error first when non-nil. It
// pairs with the named err return of the engine's sweep/set/unit functions:
// `defer func() { endSpan(span, err) }()` captures the value being returned.
func endSpan(span trace.Span, err error) {
	if err != nil {
		recordSpanErr(span, err)
	}
	span.End()
}

// recordSpanErr marks span errored without ending it, for functions whose
// returns are not named (the span ends via a plain defer).
func recordSpanErr(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// unitSpanAttrs is the shared attribute set for a unit span.
func unitSpanAttrs(ref UnitRef) []attribute.KeyValue {
	prov, model := splitUnitKey(ref.Key)
	return []attribute.KeyValue{
		attribute.String("skill", ref.Skill),
		attribute.String("provider", prov),
		attribute.String("model", model),
		attribute.String("kind", kindAttr(ref.Kind)),
	}
}

// splitUnitKey splits a "provider/model" unit key into its parts.
func splitUnitKey(key string) (provider, model string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

// kindAttr names a unit's tier for span attributes.
func kindAttr(k Kind) string {
	if k == KindTriggers {
		return "triggers"
	}
	return "evals"
}
