// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"cmp"
	"fmt"
	"io"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// VersionPin returns the repository's configured evolve version constraint
// (config key "version", terraform-style: "0.4.0", "~> 0.4", ">= 0.4, < 1"),
// or "" when the repository does not pin. Like every config key it also
// resolves from the environment (EVOLVE_VERSION).
func (o *Options) VersionPin() string {
	return strings.TrimSpace(o.Viper.GetString("version"))
}

// CheckVersionPin verifies that binary (the running evolve's version) satisfies
// the repository's version pin, gating the commands that rewrite results or
// reports so one contributor's evolve upgrade cannot force everyone else's. No
// pin passes; an unparseable pin is a config error. Only release builds are
// checked: a binary version that is not semver ("dev") or carries a prerelease
// (git-describe and goreleaser snapshots) warns to w and passes, matching
// terraform's required_version — the pin polices released upgrades, not
// from-source builds, and go-version constraints reject every prerelease
// outright.
func (o *Options) CheckVersionPin(binary string, w io.Writer) error {
	pin := o.VersionPin()
	if pin == "" {
		return nil
	}
	constraints, err := goversion.NewConstraint(pin)
	if err != nil {
		return fmt.Errorf("config: invalid version constraint %q: %w", pin, err)
	}
	v, err := goversion.NewVersion(binary)
	if err != nil || v.Prerelease() != "" {
		fmt.Fprintf(w, "warn: evolve %s is not a release build; skipping the repository version pin %q\n",
			binary, pin)
		return nil
	}
	if !constraints.Check(v) {
		return fmt.Errorf("evolve %s does not satisfy this repository's version pin %q (%s); "+
			"install a matching evolve release or update the version key",
			binary, pin, cmp.Or(o.ConfigFileName(), "config"))
	}
	return nil
}
