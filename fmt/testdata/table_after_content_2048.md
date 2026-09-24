### 1.4 The vision, distilled
1. A Kanto-first campaign inside the HGSS engine: FRLG-faithful through Mt. Moon, then the timeline break, a Cyrus endgame, and Johto as post-game.
2. A decomp-native toolkit (the forges) as the only path from author to ROM.
3. Agent-playable QA: every claim backed by a live run on the exact ROM hash.
4. The MMO plane is dormant. `SPACE.md` and `initiatives/browser-pokemmo.md` still describe it; both are stale against the September direction.
## 2. The actual state today
### 2.1 Component table
Status values: working, partial, stubbed, planned, dormant.

| Component | Status | Evidence | Notes |
|---|---|---|---|
| forge (integrator) | working | `tools/forge/README.md`; 14 `_test.go` files in `tools/forge/`; `hacks/kanto-first/hack.json` | Apply, build, verify, boot smoke, scenarios, restore. Caveats: builds are not byte-reproducible across trees (README R1 note); restore clobbers a dirty vendor tree; one shared tree, one lock (`scripts/forge-build-lock.sh`). |
| poryscriptZ (forge 1) | working, not adopted | `/home/draco/work/poryscriptz/` (21 Go files, 8 test files, parity tests); vocab at `tools/forge/vocab/heartgold/{scrcmd,macros}.json` | v0.1 and v0.2 shipped with byte parity. Zero `.poryz` files exist under `hacks/kanto-first/`. All 35 script overlays under `files/fielddata/script/scr_seq/` are hand-authored `.s`. |
| dataforge (forge 2) | working | `tools/forge/dataforge/validate_test.go`; overlays `files/poketool/trainer/trainers.json`, `files/fielddata/encountdata/gs_enc_data.json` | Validate-on-apply against constants headers. Edit primitives stay minimal (level-scale in spec). Authors hand-edit JSON. |
| patchforge (forge 3) | partial | `game-patches/heartgold/modules/{runtime-rules,bugfix-catalog}/module.json`; `scripts/apply-heartgold-patch.sh`; 7 C overlays under `hacks/kanto-first/src/` | C overlays copy through as `csource` with no validation. No catalog CLI. The hack still requires the MMO module `runtime-rules` (`hack.json`). |
| MMO plane | dormant | See 2.3 |  |
