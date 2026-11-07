# Basalt

A turn-based duel on a causeway of hexagonal basalt columns. Every stone you
place shoves the rival stones touching it one hex away, and anything shoved off
the edge sinks into the sea and takes its column with it.

Built with [Wails v3](https://v3.wails.io) (Go + WebView), React and TypeScript
for Windows, macOS, Linux and Android. The rules engine and the AI live in Go;
the frontend only renders what Go returns.

## Rules

The board is a hexagon of 61 columns (radius 4). Lava moves first, then Ash.

1. **Place.** On your turn put one stone on any empty column.
2. **Push.** The new stone pushes every touching rival stone one hex straight
   away from it. A stone with anything behind it (a stone of either colour)
   stays put. Pushes never chain, and your own stones never move.
3. **Sink.** A stone pushed off the outer edge sinks. The column it stood on
   crumbles into the sea, leaving a gap. A stone later pushed into a gap also
   sinks (the gap does not widen).
4. **Win** by making a straight row of **five** of your stones, or by sinking
   **five** rival stones. Your own win is checked first; if your push completes
   your rival's row instead, they win.
5. If no empty column is left, whoever has more stones standing wins; equal
   counts are a draw.

### How the rules were tuned

The starting proposal (row of four, six captures) was played out AI-vs-AI in Go
(`internal/engine/balance_test.go`, MCTS 3,000–8,000 playouts per move):

| Variant | Games | First / second player wins | Avg. plies | Won by row / by sinking |
|---|---|---|---|---|
| Row of 4, sink 5, crumbling rim | 40 | 26 / 14 | 16.4 | 40 / 0 |
| Row of 5, sink 4, crumbling rim | 40 | 19 / 21 | 23.5 | 13 / 27 |
| Row of 5, sink 5, no crumbling | 40 | 22 / 18 | 28.9 | 28 / 12 |
| Row of 5, sink 6, crumbling rim | 40 | 18 / 22 | 31.3 | 30 / 10 |
| **Row of 5, sink 5, crumbling rim (shipped)** | 3 × 30 at 8,000 playouts | **44 / 46** | **27.9–31.2** | **50 / 40** |

A row of four ended every game in about eight moves each and made pushing
irrelevant, so the row is five. With five it is balanced between the two ways
to win, games last about 30 plies, and no draws occurred. The first player
showed no measurable advantage (44–46), so no swap rule or komi is used; a
pie-rule variant was also tried and the AI declined the swap in 22 of 30 games.
The **crumbling rim** is Basalt's twist: every capture reshapes the coast,
opening new holes to push into and breaking rows that would run through them.

## Features

- **Play the computer** at three levels, as Lava, Ash or a random side.
- **Two players** on one device (hot-seat), with an optional *face-to-face*
  mode that turns the far player's panel toward them on a phone.
- **Daily puzzle**: a "win in two" position generated from the UTC date by the
  Go engine, verified to have exactly one winning first move.
- Undo, hints (the AI suggests a move), move history with push/sink counts,
  resign, and a saved game that survives closing the app.
- Hover (or tap) previews the placement *and* every push: arrows for slides,
  red rings for stones that would sink, a bar for blocked pushes, and the row
  that would win.
- Animated drop, push, sink, splash and column collapse; faint trails keep the
  last move readable afterwards. Reduced motion is respected.
- Profile: wins, losses, draws and streaks per level, longest streak, stones
  sunk, two-player games, puzzles solved and puzzle streak, nine achievements.
- Procedural WebAudio sound (no audio files) and native haptics on mobile.
- English, Russian and Uzbek (Latin script, with ʻ U+02BB and ʼ U+02BC).

## The AI

Monte Carlo tree search with UCT (`internal/engine/mcts.go`):

- Tree nodes are expanded with exact tactics: if the side to move can win at
  once only that move is considered, otherwise moves that hand the opponent an
  immediate win are pruned. Wins are exact because a row can only be completed
  by the placed stone and captures depend only on the stones next to it.
- Playouts use a light policy: win now, block an open row, sometimes sink a
  stone, otherwise play near existing stones.
- Searches are bounded by time and/or iterations, run off the UI thread, and
  can be cancelled (undo, new game, leaving, or JavaScript cancelling the call
  through the call context). With an iteration budget and one worker the
  search is deterministic for a given seed (tested).
- Root parallelism: several independent trees (one per worker) merged by visit
  counts.

| Level | Budget | Notes |
|---|---|---|
| Ember | 500 playouts | soft final choice (temperature 0.6) and a 30% chance per move to skip its win/loss-in-one checks, so it makes human-like mistakes |
| Magma | 600 ms (max 12,000 playouts) | full tactics |
| Caldera | 1.5 s, up to 4 workers | full tactics |

Measured on the development PC (`BASALT_STRENGTH=1 go test ./internal/game
-run TestStrengthReal`, 20 games per pairing, colours alternating):

| Pairing | Result | Avg. plies | Slowest move |
|---|---|---|---|
| Magma vs Ember | 20–0 | 19.1 | Magma 0.55 s, Ember 23 ms (+450 ms minimum delay in the app) |
| Caldera vs Magma | 19–1 | 29.6 | Caldera 1.51 s, Magma 0.60 s |

Single-threaded the search runs about 50,000 tactical playouts per second on
this PC, so Caldera considers roughly 75,000 playouts per worker per move. The
normal test run also plays a deterministic ladder (`TestStrengthLadder`,
iteration budgets) that checks each level beats the one below.
Because every level is budgeted by **time** (Ember by a small playout count),
Caldera answers in about 1.5 s on any CPU; a slower phone just searches fewer
playouts in that time.

## Controls

| Action | Mouse | Touch | Keyboard | Gamepad |
|---|---|---|---|---|
| Aim | Hover | Tap a column | Arrows / WASD (physical keys, any layout) | D-pad / left stick |
| Place | Click | Tap the same column again, or **Place** | Enter / Space | A |
| Undo | Button | Button | U | X |
| Hint | Button | Button | H | Y |
| Menu / back | Button | Button, Android Back | Esc | B / Start |

Menus support Tab (focus never leaves an open dialog), arrow keys inside option
groups, and gamepad spatial navigation (D-pad moves focus, A activates, B goes
back). *Tap twice to place* can be turned off in Settings.

## Project layout

```
main.go                      Wails app setup (the only Go file at the root)
internal/engine/             rules, tactics, MCTS AI, puzzle generator
  board.go rules.go          geometry, State, Play() + events
  tactics.go mcts.go         exact win/safety queries, the search
  puzzle.go                  daily "win in two" generation and defence
  testdata/vectors.json      shared rule vectors (also checked by the TS port)
internal/game/               the bound service
  gameservice.go             GameService, GameError, AI levels, haptics
  session.go                 live game, history, JSON views
  profile.go                 atomic JSON profile store
  achievements.go
frontend/src/
  App.tsx                    screens, game flow, input routing, back()
  api/backend.ts             Wails bindings adapter (+ runtime detection)
  api/preview.ts             browser-only stand-in (TS rules + greedy AI)
  game/rules.ts layout.ts    TS rules port, board geometry
  game/audio.ts              procedural sound
  ui/                        Board (SVG), GameScreen, Title, dialogs, gamepad
  i18n/                      en.ts (reference), ru.ts, uz.ts
frontend/scripts/            check-vectors.ts, check-i18n.mjs
build/                       Wails build config, icons, Android project
```

### Engineering notes

- **Go is the source of truth.** The frontend sends a cell index; Go validates
  it, returns the new state and the push/sink/collapse events to animate.
  Results, streaks and achievements are computed in Go from the game it ran, so
  the UI cannot report a fake win. Abandoning a started AI game counts as a
  resignation (the UI asks first).
- **Errors** are `GameError{Code, Message}`; Wails puts them in the JS error's
  `cause` and the UI translates the code. The Android transport drops `cause`,
  so `Error()` also ends with `[code]` and the UI falls back to reading that.
- **Profile store:** mutex, deep copy on read, the update runs on a copy and is
  discarded if it or the write fails, temp file + fsync + rename, and a corrupt
  file is kept as `profile.<timestamp>.corrupt`. The unfinished game is stored
  as its move list and replayed through the rules on load.
- **Browser preview:** chosen only when the Wails runtime is absent
  (WebView2/WebKit message handlers or the Android bridge). In the app a failing
  Go call shows an error with Retry instead.
- **Rule parity:** `npm run test:rules` replays the Go-generated vectors through
  `rules.ts`.

## Develop

Requirements: Go 1.26, Node 24, Wails CLI v3.1.0 (`wails3`).

```bash
cd frontend && npm install && cd ..
wails3 dev                          # desktop app with hot reload
cd frontend && npx vite --port 9247 # browser preview (no Go)
```

Tests:

```bash
go vet ./internal/... && go test ./internal/...    # add -short to skip AI-vs-AI games
cd frontend && npx tsc --noEmit && npm run test:rules && node scripts/check-i18n.mjs
BASALT_BALANCE=1 go test ./internal/engine -run TestBalance -v      # rule study (minutes)
BASALT_STRENGTH=1 go test ./internal/game -run TestStrengthReal -v  # level strength (minutes)
```

After changing Go service signatures: `wails3 generate bindings -clean=true -ts -i`.

## Build

```bash
wails3 build                         # bin/basalt.exe (desktop, current OS)
wails3 task android:package:fat      # arm64 + x86_64 APK (shipped as bin/basalt-universal.apk)
wails3 task android:package          # arm64 APK, bin/basalt.apk (delete
                                     # build/android/app/src/main/jniLibs/x86_64 first)
```

On Windows the generated `build/android/Taskfile.yml` has been patched to work
with the Windows NDK toolchain. `ANDROID_HOME` and `JAVA_HOME` (Android
Studio's `jbr` works) must be set.

### Signing

Without `ANDROID_KEYSTORE_FILE`, `ANDROID_KEYSTORE_PASSWORD`,
`ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` the release APK is signed with
the Android debug key: fine for sideloading, not accepted by Google Play. The
application id is `com.basalt.game`. The APK requests only Internet, vibration
and network-state permissions (the template's camera, location, biometric,
notification and foreground-service entries were removed), and the activity
pads the WebView clear of the system bars for Android 15+ edge-to-edge. Windows
builds are unsigned.

### macOS and iOS

Needs a Mac with Xcode.

```bash
wails3 task darwin:package             # bin/basalt.app, arm64, ad-hoc signed
wails3 task darwin:package:universal   # same, arm64 + amd64
wails3 task ios:package                # bin/basalt.app for the iOS Simulator (iOS 15+)
wails3 task ios:deploy-simulator       # package, install and launch on the booted simulator
```

- Both packages are written to `bin/basalt.app`, so move one aside before
  building the other platform.
- The first macOS build in a fresh clone regenerates `build/darwin/icons.icns`,
  `build/darwin/Assets.car` and `build/windows/icon.ico` from
  `build/appicon.icon`, which still holds the Wails template icon. The committed
  files are the real Basalt icons; restore them with
  `git checkout -- build/darwin/icons.icns build/darwin/Assets.car build/windows/icon.ico`.
  Task caches the checksum in `.task/`, so later builds leave them alone.
- `build/ios/xcode/` is regenerated from `build/config.yml` and
  `build/appicon.png` on every iOS build and is not committed.
- A real iPhone needs real signing and a provisioning profile:
  `wails3 task ios:package:ipa IOS_PLATFORM=device CODESIGN_IDENTITY="Apple Development: …" PROVISIONING_PROFILE=path/to/profile.mobileprovision`,
  or `wails3 task ios:xcode` to let Xcode manage signing.
