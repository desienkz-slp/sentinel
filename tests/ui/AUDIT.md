# UI regression verification report

## Final verification

Verified against the completed, uncommitted backend fixes and shared Settings shell on **2026-10-04**. This task modified only `tests/ui`; application changes were supplied by the backend/frontend owners. No commit was created.

Command from `tests/ui`:

```sh
npm test && npm run test:summary
```

**Exit 0. All 48 tests passed: 2 Node unit tests + 46 Playwright Chromium tests.**

| Result | Count |
| --- | ---: |
| Playwright ordinary passes | 46 |
| Included `@audit` regressions | 7 |
| Expected-failure allowances | 0 |
| Failed / skipped / flaky | 0 / 0 / 0 |
| Report-level errors | 0 |
| Node endpoint-B unit passes | 2 |

Playwright JSON start time: `2026-10-04T14:13:20.382Z`; duration **78.694 seconds**. Counts were aggregated from `test-results/results.json` by `support/summarize.js`, not inferred from the list reporter. Node counts are from its TAP output and are not included in the Playwright JSON/HTML report.

Environment: Node 22.23.2, Playwright 1.61.1, Chromium, Windows. Global setup successfully rebuilt the current Go worktree with embedded assets. The parent reported full Go/race verification separately; this UI task did not rerun or claim ownership of those checks.

## Former findings now blocking regressions

| ID | Verified behavior |
| --- | --- |
| AUTH-01 | Logout rejects replay of the removed session token with 401; another independently issued session remains valid. Browser login/logout from dashboard and Settings also verifies cookie removal and copied-token rejection. |
| API-01 | Endpoint-B ping selects saved B credentials using body aliases `b`, `reasoning`, `codex`, and query `endpoint=b`. Default A remains distinct. Browser B ping omits blank keys, succeeds using saved B, honors an explicit wrong-key override, recovers with the correct key, and does not persist ping values. An unsaved B URL receives neither saved credential. |
| API-02 | The real browser diagnosis POST sends `stream:true` and receives HTTP 200 SSE with exactly one report and no error event. The rendered UTF-8 answer, escaped markup, enabled run button and history match the actual persisted report. No transport substitution is used in this test. |
| API-03 | Recipe deletion supports cancel and wrong-PIN rejection, then completes with authorized HTTP 200 and visible success. Subsequent reads do not block, the JSON store loses the recipe, restart preserves deletion, and another DELETE returns 404. PIN fields clear. |
| UI-01 | Settings loads one shared shell, marks Pengaturan active, and all five dashboard destinations work, including diagnosis/WhatsApp reveal and return to Settings. Desktop main/sidebar geometry does not overlap. |
| POLICY-01 | Both REMOVE and empty decisions reject wrong PIN, remove the selected rule, retain deletion markers across repeated saves and a fresh page, survive a process restart, retain another rule's edited permission, and never modify baseline policy bytes. Two separate parameterized browser tests. |

All five old failure annotations and their environment-dependent allowances were removed. `test:audit:strict` remains a backward-compatible command with the same strict assertions as `test:audit`. No skip, retry or conditional success fallback replaces an old allowance. `forbidOnly` prevents accidental focused runs; `test:summary` rejects allowances, skips, flakiness and failures in the latest report.

## Added harness and coverage

- Isolated server restart keeps the same disposable config/stores; read-only helpers inspect recipe JSON, policy overlay and baseline files.
- Responsive interactions pass at **1440×900, 768×1024 and 390×844**. Narrow screens use the actual shared horizontal scrollable navigation rather than an assumed hidden sidebar.
- Agent B's `settings-b-ping.test.cjs` is included in `npm test` and can run separately with `npm run test:unit`. Its two VM tests verify omitted/explicit keys with blank saved URL/model fields; browser success coverage remains independent and uses real Go handlers and a local provider.
- The first adaptation run found stale test assumptions about explicit B keys and mobile shell visibility, plus Chromium's charset-less SSE protocol text decoding. These were corrected to the implemented contracts before the final full run. For SSE, report identity/framing are checked from the stream, while the authoritative persisted JSON report and exact synthetic Unicode answer validate rendered text. The separate CRLF/final-frame transport test remains explicitly synthetic.

## Artifacts and rerun

- `test-results/results.json`: full 46-test Playwright machine report.
- `playwright-report/index.html`: full Playwright HTML report (`npm run report`).
- `npm run test:summary`: verifies and prints the latest report totals without replacing it.
- `npm test`: runs both Node tests and all Playwright tests again.
- `npm run test:audit` / `npm run test:contracts`: optional filtered runs; these replace the current Playwright report.

Generated artifacts are ignored by git. No real provider, WhatsApp delivery/pairing, updater installation, database or network-equipment operation is exercised. This is functional Chromium/layout coverage, not screenshot-diff, full accessibility or cross-browser certification. No unresolved integration finding was observed in the final suite.
