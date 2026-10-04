// Authentic persisted schema: internal/caseengine/persist.go (version 1).
// Event/Verification use exported Go field names, NOT the lowercase API view.
const at = '2026-01-01T12:00:00Z';
export const trackerSeed = {
  version: 1,
  active: ['INVESTIGATION', 'ESCALATION', 'HUMAN_HANDLING', 'FAILED', 'RESOLVED'].map((state, i) => ({
    id: `CASE-20260101-UI000${i}`, channel: 'whatsapp', identity: `ui-customer-${i}`,
    created_at: at, updated_at: at, state, version: 2,
    events: [{ From: 'NEW', To: state, Actor: 'ui-fixture', Reason: 'Recorded <b>evidence</b>', At: at }],
    verifications: state === 'RESOLVED' ? [{ Passed: true, Source: 'fixture-probe', Summary: 'Verified <b>literal</b>', At: at }] : [],
  })),
  archive: [],
};

// Wire schema: internal/observability/casekpi.go. Zero verified must remain zero.
export const unverifiedKPI = {
  total_cases: 9, active_cases: 3, resolved: 6, escalated: 0, failed: 0,
  human_handling: 0, verified_resolved: 0, avg_resolution_ms: 0,
  escalation_rate_percent: 0, resolution_rate_percent: 66,
};

// wa-gateway/app/qr.js response fields; the PNG is a synthetic 1px image.
export const qrPayload = {
  qr: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a9WQAAAAASUVORK5CYII=',
  expires_in: 45, is_expired: false,
};
