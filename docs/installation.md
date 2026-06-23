# Installation

`evolve` is a single Go binary.

## Homebrew (macOS and Linux)

```sh
# Homebrew 6 requires end-users to trust formulae / casks in a tap
brew trust --cask bitwise-media-group/tap/evolve
# ...or trust all future formulae / casks in our tap
brew trust --tap bitwise-media-group/tap

# install the cask from the tap
brew install --cask bitwise-media-group/tap/evolve
```

The cask lives in [bitwise-media-group/homebrew-tap](https://github.com/bitwise-media-group/homebrew-tap), is updated by
every release, and installs shell completions alongside the prebuilt binary.

## Go install

As an alternative, build from source with your Go toolchain:

```sh
go install github.com/bitwise-media-group/evolve/cmd/evolve@latest
```

The resulting binary reports its version as `dev`, and shell completions are not installed. To build this checkout
instead:

```sh
make build
./evolve version
```

## Release binaries

Tagged [releases](https://github.com/bitwise-media-group/evolve/releases) ship prebuilt `evolve_<version>_<os>_<arch>`
archives for Linux, macOS and Windows on amd64 and arm64 — with `checksums.txt`, a Sigstore bundle per binary, and an
SPDX SBOM per archive. Download, extract, and put `evolve` on your `PATH`.

## Verify the environment

Run `evolve doctor` from a plugin repository to check provider CLIs, credentials and token-counting access:

```sh
evolve doctor
```
