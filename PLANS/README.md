# TVProxy Modular Refactor — Execution Plans

## Principle
**Modularity protects working code.** Each component has clean boundaries. Changing one output plugin cannot break another.

## Phase Overview

| Phase | Plans | Description | Risk |
|-------|-------|-------------|------|
| 1 — Foundation | 02-06 | Create plugin packages with interfaces. Zero changes to existing code. | None |
| 2 — Recording | 07-10 | Fix broken recordings. Always-recording + manual preserve. | Medium |
| 3 — DecodeBridge | 11-14 | Extract decode/encode, eliminate duplication. One decode → N outputs. | High |
| 4 — Source Plugins | 15-20 | Wrap existing services as Source plugins. Unified source management. | Low |
| 5 — Session Refactor | 21-25 | Clean session management. Consumers = output plugins. | Medium |
| 6 — Cleanup | 26-30 | Delete dead code, update docs. | Low |
| 7 — Future | 31-40 | WebRTC, DASH, timeshift, series recording, disk management. | N/A |

## Execution Order for Overnight Session

**Priority 1 — Get recordings working (Plans 07-10)**
This is broken and was paid for. Fix it first.

**Priority 2 — Foundation interfaces (Plans 02-06)**
New packages only, zero risk to existing code. Sets up the structure.

**Priority 3 — DecodeBridge (Plans 11-14)**
The big refactor. Eliminates pipeline duplication. Enables fan-out.

**Priority 4 — Source plugins (Plans 15-20)**
Lower risk, good cleanup. Can run in parallel with Phase 3.

**Priority 5 — Session + Cleanup (Plans 21-30)**
After everything else is stable.

## Interface Documents
- [00-SOURCE-PLUGIN-INTERFACES.md](00-SOURCE-PLUGIN-INTERFACES.md)
- [01-OUTPUT-PLUGIN-INTERFACES.md](01-OUTPUT-PLUGIN-INTERFACES.md)

## Key Design Document
- [../RECORDING-DESIGN.md](../RECORDING-DESIGN.md) — Full system architecture
