# tm board — TUI design notes

Notes from a design discussion about making `tm board` an interactive TUI.
Captures the problem we started with, the framework choice, and the shape
of the implementation. Not a spec — a record of reasoning to inform whoever
picks this up next.

## The problem we noticed

`tm board` today renders 4 kanban columns at a hardcoded ~50-column width,
regardless of the actual terminal size. On a standard 80-col terminal the
board overflows; on a 270-col terminal there's significant unused whitespace
inside each column and card subjects truncate to `…` at ~45 chars even
though there's space to render the full text.

Concretely: the tool isn't asking the terminal how wide it is. It just
`fmt.Println`s a fixed-width box layout.

## Two paths considered

### Option A — keep the static-output model, just make it width-aware

Read terminal width via `golang.org/x/term`, divide across 4 columns, render.
~30 lines of Go. Output stays composable: `tm board | less`, paste from
scrollback, redirect to a file all keep working. No alt-screen, no input
loop, no resize handling — the snapshot model.

### Option B — full interactive TUI like k9s

Alt-screen takeover, raw-mode input, live navigation across cards, state
transitions via keystrokes, possibly live refresh. Different tool entirely;
breaks composability but offers a much richer UX.

**We picked Option B.** The reasoning: state transitions (moving cards
across columns) is *the* feature of a board, and a static view can only
display state — not let you act on it. If we're touching this, the
interactive version is the one worth building.

## Framework choice: bubbletea vs tview

Both are mature Go TUI frameworks. The honest comparison:

| Dimension | tview | bubbletea |
|---|---|---|
| Battle-tested | k9s, lazygit, gh-dash. Older. | Newer (2020+), but charm tools, soft-serve, glow use it. |
| Mental model | Widget-based, imperative. `Flex`/`Table`/`Modal`. GTK-like. | Elm-style: state → update → view. Pure functions. |
| Built-in widgets | Rich: `Table`, `Form`, `Modal`, `Pages`, `Grid`. | Sparser. Compose primitives from `lipgloss` (styling) + `bubbles` (`textinput`, `list`, `viewport`, `table`). |
| Async events | Manual `app.QueueUpdateDraw` from goroutines. | Native: `tea.Cmd` returns a `tea.Msg`, runtime calls `Update`. |
| Testing | Hard. Stateful, tied to screen. | Easy. Models are pure; assert on `Update(model, msg) → newModel`. |
| Binary weight | ~800 KB | ~1.5-2 MB |
| Aesthetic | Classic curses. | Polished out of the box. `lipgloss` makes borders/colors trivial. |

### Why bubbletea for `tm`

1. **Small surface, touched by humans + agents.** The Model/Update/View
   shape reads cleaner to someone landing in the code cold. The board is
   ~one file, not a multi-pane app.
2. **State transitions are the feature.** Moving a card from backlog →
   in_progress is a clean Msg + state mutation in Tea. tview wires the
   keystroke handler to mutate a table cell + redraw + call the backend
   — more glue.
3. **Live updates plumbed naturally.** If `tm` later wants the board to
   react to journal events (agent moves a card → board redraws), Tea's
   `tea.Msg` channel handles it without new plumbing.
4. **Testing transitions matters for an agent-touched tool.** Tea lets
   you unit-test "key X in state Y produces state Z" without spinning a
   terminal.

### Why this is NOT obvious

- "k9s uses tview, so tview is safe" — k9s is a complex multi-pane
  resource-streaming app where tview's widget library does real work.
  `tm board` is much simpler. tview's strengths don't apply as much.
- "Tea is prettier" — true but cosmetic.
- "Tea is more popular now" — popularity isn't a technical argument.

### When tview would be the right call

- You already know tview from another project. Familiarity beats
  theoretical fit.
- You want a working board in one afternoon. tview's `Table` widget +
  4-column `Flex` gets there faster than rebuilding the equivalent in
  lipgloss.
- You see `tm` growing into something k9s-shaped — multiple panes,
  resource browser, log tail. tview's widget library scales there.

## Bubbletea — the implementation shape

A sketch in ~150 lines covers the v1: render board, navigate with
arrows/hjkl, move cards across columns, refresh, help modal. The
structure:

```go
type model struct {
    repo Repo
    width, height int       // updated on tea.WindowSizeMsg
    columns map[string][]Task
    colIdx, rowIdx int
    err error
    showHelp bool
}

func (m model) Init() tea.Cmd { /* loadTasksCmd */ }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // switch on msg type:
    //   tea.WindowSizeMsg  → store width/height
    //   tasksLoadedMsg     → rebuild columns map
    //   taskMovedMsg       → reload
    //   errMsg             → surface error
    //   tea.KeyMsg         → navigate / move card / toggle help
}

func (m model) View() string {
    // lipgloss layout: 4 columns side-by-side, computed from m.width
    // overlay help modal via lipgloss.Place() when m.showHelp
}
```

The full sketch is in the conversation history — not copied here because
the patterns are more interesting than the exact code.

## Key design choices in the sketch

### 1. The keymap registry is load-bearing

Single source of truth for both `Update` (what each key does) and `View`
(what the help modal shows):

```go
var keyBindings = []keyBinding{
    {keys: []string{"↑", "k"}, help: "select card above"},
    {keys: []string{"shift+L"}, help: "move card to next column"},
    {keys: []string{"?"}, help: "toggle this help"},
    // ...
}
```

Without this, two places need to agree on keybindings: the `Update` switch
and whatever's hardcoded into help text. They drift the moment someone
adds a key. The registry makes them the same data. (Could go further and
put the handler `func(m model) (model, tea.Cmd)` in the registry too —
fully data-driven keymap. Pretty for medium-sized apps, marginal for
`tm board`'s ~10 keys.)

### 2. Modals are state flags, not pushed screens

Help is `showHelp bool` in the model. Not a nested view, not a stack.
Help isn't somewhere you *go* — it's something that pops up over where
you are. A boolean captures that intent exactly. When help closes you're
back where you were, automatically.

### 3. Modal input lockout is one branch, not per-case guards

When help is open, the entire navigation keymap should be inert:

```go
case tea.KeyMsg:
    if m.showHelp {
        switch msg.String() {
        case "?", "esc", "q":
            m.showHelp = false
        }
        return m, nil
    }
    // ... normal navigation ...
```

The alternative — guarding every case with `if !m.showHelp` — drifts the
moment you add a new key and forget the guard. One branch, one place to
reason.

### 4. lipgloss.Place() for overlays

True z-ordering doesn't exist in terminals. lipgloss.Place(width, height,
hAlign, vAlign, content) centers content in a frame and overwrites the
cells underneath. The board "disappears" behind the modal because Place
overwrote those cells. Clean primitive; no manual padding math.

### 5. Async is just messages

```go
func loadTasksCmd(repo Repo) tea.Cmd {
    return func() tea.Msg {
        tasks, err := repo.List()
        if err != nil { return errMsg{err} }
        return tasksLoadedMsg{tasks}
    }
}
```

Returns a `tea.Cmd` (a function). Tea runs it off the main loop and
delivers the result back via `Update`. The UI stays responsive while
the DB read happens. No goroutine plumbing, no channel wiring, no
"is this safe to call from a callback" anxiety.

## What additive features look like

Each is ~10-30 lines on the skeleton, slotted in by adding a Msg type
+ Update case + View branch. **None require rearchitecting.**

- **Detail view on Enter** — push a "viewing task X" state, render that
  in View, `esc` pops back. ~20 lines.
- **Filter input (`/` key)** — `bubbles/textinput` widget composed into
  the model. ~30 lines.
- **Live refresh** — `tea.Tick(5*time.Second, ...)` Cmd that returns a
  refresh message every 5s. ~5 lines.
- **Confirm modal for destructive moves** — `confirmingMove *Task` field,
  another modal branch in View. ~20 lines.
- **First-run hint** — `m.helpShownBefore bool` persisted to
  `~/.tm/state.json`. If false on first launch, open help automatically.
  ~20 lines including persistence.
- **Contextual help** — filter `keyBindings` against current state so
  modal shows only relevant keys. ~5 lines.

## Honest pain points to expect

- **`lipgloss.JoinHorizontal` with mixed heights** — if one column has
  12 cards and another has 2, short columns don't pad automatically.
  Pad with empty strings or use `Height()` constraints per column.
- **No built-in viewport scrolling** for long columns. If a column has
  50 cards on a 30-row terminal, slice the visible range yourself or
  integrate `bubbles/viewport`.
- **Selected-card tracking across column switches** — model row-index
  is global; you may want per-column row positions so jumping right
  and back returns to the same card.

## What this confirms

Adding the help modal didn't require touching anything that wasn't help-
related. Board renderer the same, data flow the same, async commands
the same. New feature was additive at the seams: one model field, one
input-handler branch, one View branch.

That's the property you want from a UI framework choice. If adding help
had required restructuring the keymap, threading state through three
widgets, or wiring a focus stack — that's a sign the framework is fighting
the use case. Tea wasn't fighting.

## The actual recommendation

**bubbletea + lipgloss + bubbles** for `tm board`. But: if after reading
the sketch the mental model feels foreign, **tview is genuinely fine** —
there's no shame in picking the framework whose model matches how you
already think. Both will produce a working `tm board`; the only wrong
choice is the one you'll resent maintaining.

## Out of scope (intentionally)

- Whether `tm board` static output should also stick around (as `tm board
  --static` or a separate command) for scripting use cases. Likely yes,
  but a separate decision.
- Theming / color customization. Lipgloss makes this trivial later; not
  worth designing for in v1.
- Mouse support. Tea supports it (`tea.WithMouseAllMotion`), but kanban
  via keyboard is the primary interaction and mouse can come later.
