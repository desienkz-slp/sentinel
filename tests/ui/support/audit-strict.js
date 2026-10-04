import { spawnSync } from 'node:child_process';
// Compatibility entry point: all audits are now strict in every run.
const result = spawnSync(process.execPath, ['node_modules/@playwright/test/cli.js', 'test', '--grep', '@audit'], { stdio: 'inherit' });
if (result.error) throw result.error;
process.exit(result.status ?? 1);
