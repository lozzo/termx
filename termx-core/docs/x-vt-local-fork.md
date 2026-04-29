# Local `x/vt` Fork

`termx` now carries a local copy of `github.com/charmbracelet/x/vt` at:

`third_party/github.com/charmbracelet/x/vt`

Root `go.mod` uses:

```go
replace github.com/charmbracelet/x/vt => ./third_party/github.com/charmbracelet/x/vt
```

## Why this layout

- `vendor/` is generated and easy to overwrite
- a normal checked-in directory is easier to patch and review
- the nested module keeps the original import path, so existing `import github.com/charmbracelet/x/vt` lines do not change

## Intended use

This local fork is for `termx`-specific terminal damage work, especially:

- exposing a consumable damage stream from emulator writes
- carrying richer move/scroll/clear semantics than `Touched()` rows alone
- iterating on API shape without waiting on upstream

Current fork-specific API:

- `(*vt.Emulator).WriteWithDamage([]byte) (int, error, []vt.Damage)`
- `(*vt.SafeEmulator).WriteWithDamage([]byte) (int, error, []vt.Damage)`

Current fork-specific damage set:

- `vt.SpanDamage`
- `vt.ClearDamage`
- `vt.ScrollDamage`
- `vt.MoveDamage`

## Maintenance rule

- sync from upstream into `third_party/github.com/charmbracelet/x/vt` intentionally
- keep `LICENSE` and upstream module path intact
- keep `termx`-specific changes small and documented in commits so future rebases stay manageable
