// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package configdoc renders the configuration documentation: a Markdown
// reference describing every option with its default, and annotated example
// config files (.evolve.yaml, .evolve.jsonc, .evolve.toml) with every
// default value set. The schema pulls defaults from the same code the
// engines use, so the generated docs cannot drift from the runtime.
package configdoc
