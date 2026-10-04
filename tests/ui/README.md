# Frontend browser regression tests

Standalone Playwright package. All test/harness changes live under `tests/ui`; no web/backend changes or commits are required.

## Run

Prerequisites: Go matching the root module, Node.js 22+, npm, Chromium dependencies.

```sh
cd tests/ui
npm ci
npx playwright install chromium
npm test                  # 2 Node tests, then the full Playwright suite
npm run test:summary      # validate/aggregate the latest Playwright JSON report
npm run test:unit         # only the 2 endpoint-B payload tests
npm run test:contracts
npm run test:audit
npm run test:audit:strict
npm run report
```

On Linux CI use `npx playwright install --with-deps chromium` if browser system dependencies are missing. Run the Go baseline separately from the repository root: `go test ./...`.

Every Playwright test is an **ordinary blocking regression**, including the seven tagged `@audit`. There are no expected-failure allowances, skipped tests, or retries; focused `.only` tests are forbidden. `test:audit:strict` is retained as a compatibility alias with the same behavior as `test:audit`—no environment switch relaxes assertions. `test:summary` reads the latest JSON report and returns nonzero for failures, skips, flakiness, allowances, or report errors. It reports Playwright counts only; the two Node tests are reported separately by TAP during `npm test`.

`settings-b-ping.test.cjs` runs the actual `pingEndpointB` function in a Node VM with stubbed DOM/API helpers to verify omitted/explicit key payloads with blank saved fields. It complements, but does not replace, browser-to-Go-to-provider coverage.

## Isolation / real contracts

- Global setup builds the current Go worktree, including its embedded HTML/CSS/JS. Each test starts a fresh binary on a dynamic loopback port.
- Disposable config, authentication, case store, recipe store, policy overlay, and data directories are inside ignored `.runtime/`. Recipe/policy tests restart the same isolated process/config to verify persistence. Test teardown kills its process and removes its directory.
- Only synthetic test credentials are used. Production config files are not read/copied. Provider/database/proxy environment overrides are sanitized.
- A loopback HTTP provider validates distinct A/B keys and implements model listing/chat responses. Real Go handlers, persistence and browser payloads are exercised; this is not a mock frontend server.
- WhatsApp and updater are disabled. Router testing uses loopback port 1; manual probes use a rejected documentation address. No real message, update installation or network-equipment operation is performed.
- Browser requests to other origins are blocked. No external QR service is allowed.
- Intentional response/transport substitutions are labeled in the tests: API/network failures; KPI type/zero edges; QR states; alert/update availability; SSE framing. The CRLF/final-frame transport test uses a report obtained from the actual **nonstream** Go API, not a fabricated report schema. Separately, API-02 exercises unmodified browser-to-Go SSE, validates report framing/identity, UTF-8 answer rendering and history via the actual persisted JSON report. Chromium's protocol text view may decode charset-less SSE as Latin-1; rendered Unicode is checked against JSON, not that text view.

## Coverage map

| Area | Coverage |
| --- | --- |
| Login | required fields, bad credentials, transport failure, trimmed username, real cookie/session, logout from both pages with copied-token rejection and independent-session preservation, show/hide password |
| Dashboard | empty/populated cases, search, all four filters, details/evidence escaping, close/Escape/focus return, refresh/failure recovery, strict numeric KPIs including verified zero |
| Diagnosis | exclusive route open/leave, readonly configured model, real SSE success, authentic-report SSE rendering with CRLF/final frame, payload contract, probe selection/validation/allowlist rejection, clear/history |
| WhatsApp | drawer, gateway controls and destructive confirmations, status/QR errors, PNG data-only QR/expiry/rejection, recipient/text required, send cancel/confirm, blocklist normalization/dedupe/removal, persisted flags |
| Settings | save/reload, omitted-secret preservation, explicit URL clearing, both model lists/manual prompts, wire protocols, A/B ping success, B body/query selector aliases, saved-key selection, explicit overrides and unsaved URL credential isolation, boot retry/validation, all six team flags, staff search/reset/CRUD, router fields/save/check/remove, integration errors |
| Privileged dialogs | rule cancel/reload/authorization rejection, REMOVE/empty-decision persistence across repeated saves, fresh documents and process restarts without baseline edits; recipe cancel/wrong PIN/authorized deletion, disk persistence, restart and subsequent reads/deletes, required authorization inputs and PIN reset, Escape |
| Navigation | exclusive dashboard panels and Settings section groups, one active link, direct hashes/reload, back/forward, same-route selection, unsaved diagnosis/case/WhatsApp/Settings drafts, shared AI save-bar scope and combined endpoint/agent payload |
| Other controls | section navigation/reloads, shared dashboard/Settings shell links and active state, alerts disclosure/escaping, update cancel/confirm with disabled real updater |
| Responsive | 1440×900, 768×1024, 390×844; page overflow, diagnosis panel, case/blocklist/admin dialogs, focus, login toggle, desktop dashboard/Settings sidebar separation and narrow horizontal navigation |
| API-only | methods, malformed JSON, redaction, embedded assets/cache headers, dashboard/settings read schemas, distinct A/B model-key selection, unauthorized writes |

This is Chromium functional/layout regression coverage, not screenshot-diff certification, a complete accessibility audit, or real mobile-device/cross-browser testing. Real provider availability, successful WhatsApp pairing/delivery, updater installation/restart, and successful router probes are intentionally out of scope.

## Artifacts

`test-results/results.json`, HTML `playwright-report/`, failure traces and disposable Go/provider logs are ignored by git. Failure traces are retained automatically. Each Playwright run replaces the reports, so run `test:summary` before replacing a full-suite report with a filtered run. Never point the fixture at production.

See [AUDIT.md](AUDIT.md) for reproducible mismatches and verification results.
