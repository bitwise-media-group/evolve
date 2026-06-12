## evolve completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	evolve completion fish | source

To load completions for every new session, execute once:

	evolve completion fish > ~/.config/fish/completions/evolve.fish

You will need to start a new shell for this setup to take effect.


```
evolve completion fish [flags]
```

### Options

```
  -h, --help              help for fish
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

