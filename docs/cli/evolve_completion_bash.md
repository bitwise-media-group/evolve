## evolve completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(evolve completion bash)

To load completions for every new session, execute once:

#### Linux:

	evolve completion bash > /etc/bash_completion.d/evolve

#### macOS:

	evolve completion bash > $(brew --prefix)/etc/bash_completion.d/evolve

You will need to start a new shell for this setup to take effect.


```
evolve completion bash
```

### Options

```
  -h, --help              help for bash
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

