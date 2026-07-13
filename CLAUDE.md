# bisturi — conventions

- **Every user-facing feature ships in both the CLI and the TUI.** A flag on the
  `--cut` path needs the matching TUI affordance (a key + a hint in the footer/
  diff view) in the same change — don't land one and defer the other.
