# Precision Dark UI implementation plan

## Scope and invariants
- Initial scope: redesign `web/` presentation and browser-side interaction. After real-server browser tests reproduced pre-existing blockers, the user explicitly approved limited backend bugfixes (streaming response writer, recipe-store locking, session revocation, Endpoint B ping credential routing, and policy deletion persistence). Preserve existing features, permissions, and backward-compatible API contracts; no unrelated backend changes.
- Preserve existing dashboard, diagnosis/probe, WhatsApp administration, sessions/cache, update flow, endpoint configuration, integrations, team modes, staff, policies and recipes.
- Existing local UI edits are the starting point. Snapshot stored outside repository before work.
- Production UI reads existing `/api/*` endpoints only. No demonstration metrics, fabricated sparklines, placeholder incident records, invented SLA/severity or sample diagnosis messages. Distinguish missing data, loading, errors and confirmed empty results.

## Implementation
1. Audit backend contracts and every existing interactive control.
2. Shared Precision Dark design system: solid navy surfaces, restrained blue/violet accents, accessible text, responsive labelled navigation, keyboard focus and reduced-motion support.
3. Dashboard: operations heading, API refresh timestamp, KPI semantics from case engine; searchable/filterable case table; lifecycle/verification detail drawer using case API data; collapsible diagnostic workspace; full existing integration controls and supporting history.
4. Settings: section navigation, visible save/test feedback, dirty and loading protection, masked approval dialog, preserve configured models and all API fields. No implicit saved-configuration changes from misleading buttons.
5. Login: matching brand treatment, password visibility control and explicit authentication/network feedback.
6. Fix broken browser interactions without changing backend semantics. No control promises an unsupported backend capability (e.g. per-request AI model override or invented case assignment).

## Verification
- Full Go regression suite, `go vet ./...`, and race tests for the narrowly corrected modules.
- Dedicated Chromium tests against an isolated real Go server plus local provider/test-only fault fixtures. These fixtures are never shipped as production UI data.
- Responsive checks at phone/tablet/desktop sizes, console errors, loading/error/empty states, safe rendering of API strings, destructive-action confirmation, and representative buttons/forms.
- Never trigger real router changes, production broadcasts, update deployment, or staff/policy mutations to test a button. Record any live integration verification limitations.
- Review diff for unintended backend/config/secret changes. Commit only intended UI, approved bugfixes, docs and tests; push current branch without force and verify pushed SHA against remote.

## Data semantics
- Case KPI is cumulative over the tracker, not a made-up daily window.
- `verified_resolved: 0` must remain zero, not fall back to unverified resolved count.
- No alerts does not prove every dependency healthy. Label this KPI as alerts, with underlying alerts displayed.
- Configured credentials do not mean a tested healthy endpoint.
- `/api/ask` has no model override: diagnosis uses saved model; model configuration links to Settings.

## Status
Implementation and verification complete. Final independent test results and live-service limitations are recorded in `docs/ui-redesign-verification.md`.
