import { readFile } from 'node:fs/promises';

// Summarize actual outcomes, never treat expected failures as ordinary passes.
const report = JSON.parse(await readFile(new URL('../test-results/results.json', import.meta.url), 'utf8'));
const cases = [];
function collect(suites) {
  for (const suite of suites || []) {
    for (const spec of suite.specs || []) {
      for (const test of spec.tests || []) cases.push({ title: spec.title, ...test });
    }
    collect(suite.suites);
  }
}
collect(report.suites);
const counts = { total: cases.length, passed: 0, failed: 0, skipped: 0, flaky: 0, allowances: 0, audit: 0 };
for (const test of cases) {
  if (test.title.includes('@audit')) counts.audit++;
  if (test.expectedStatus !== 'passed') counts.allowances++;
  if (test.status === 'flaky') counts.flaky++;
  const status = test.results.at(-1)?.status;
  if (status === 'passed' && test.expectedStatus === 'passed') counts.passed++;
  else if (status === 'skipped') counts.skipped++;
  else counts.failed++;
}
const errors = report.errors || [];
console.log(JSON.stringify({ startTime: report.stats.startTime, durationMs: report.stats.duration, ...counts, errors: errors.length }, null, 2));
for (const test of cases.filter(t => t.status !== 'expected' || t.expectedStatus !== 'passed')) {
  console.log(`${test.projectName}: ${test.title}: ${test.results.at(-1)?.status || 'not run'} (expected ${test.expectedStatus})`);
}
if (!counts.total || counts.failed || counts.skipped || counts.flaky || counts.allowances || errors.length) process.exitCode = 1;
