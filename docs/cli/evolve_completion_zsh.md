## evolve completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(evolve completion zsh)

To load completions for every new session, execute once:

#### Linux:

	evolve completion zsh > "${fpath[1]}/_evolve"

#### macOS:

	evolve completion zsh > $(brew --prefix)/share/zsh/site-functions/_evolve

You will need to start a new shell for this setup to take effect.


```
evolve completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --json            emit machine-readable JSONL progress on stdout
      --layout string   repository layout: auto, marketplace, multi, or single (default "auto")
      --root string     repository root to operate on (default: walk up from the current directory)
  -v, --verbose         enable debug logging
```

### SEE ALSO

* [evolve completion](evolve_completion.md)	 - Generate the autocompletion script for the specified shell

