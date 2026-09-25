import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { statePath, type E2EState } from './state';

export default async function globalTeardown() {
	let state: E2EState;
	try {
		state = JSON.parse(readFileSync(statePath, 'utf8'));
	} catch {
		return;
	}
	try {
		process.kill(state.apiPid);
	} catch {
		// Already stopped.
	}
	await new Promise((r) => setTimeout(r, 500));
	execFileSync('psql', [state.adminUrl, '-qc', `DROP DATABASE IF EXISTS ${state.dbName} WITH (FORCE)`], {
		stdio: 'pipe'
	});
}
