import { mkdir } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

export const root = fileURLToPath(new URL('../../../', import.meta.url));
export const runtime = fileURLToPath(new URL('../.runtime/', import.meta.url));
export const binaryName = process.platform === 'win32' ? 'sentinel.exe' : 'sentinel';
export default async function build() {
  await mkdir(runtime, { recursive: true });
  // Build the current worktree: web files are embedded by Go, never served from a mock.
  execFileSync('go', ['build', '-o', path.join(runtime, binaryName), '.'], {
    cwd: root, stdio: 'inherit', timeout: 120_000,
  });
}
