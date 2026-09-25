import { execFileSync, spawn } from 'node:child_process';
import { mkdirSync, openSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { apiPort, webPort } from '../../playwright.config';
import { stateDir, statePath, type E2EState } from './state';

const core = resolve(import.meta.dirname, '../../../../services/core');

function psql(adminUrl: string, sql: string) {
	execFileSync('psql', [adminUrl, '-v', 'ON_ERROR_STOP=1', '-qc', sql], { stdio: 'pipe' });
}

async function waitForHealth(url: string, timeoutMs: number) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		try {
			if ((await fetch(url)).ok) return;
		} catch {
			// Not listening yet.
		}
		await new Promise((r) => setTimeout(r, 250));
	}
	throw new Error(`API did not become healthy at ${url}`);
}

export default async function globalSetup() {
	const adminUrl = process.env.E2E_DATABASE_URL;
	if (!adminUrl) throw new Error('Set E2E_DATABASE_URL to a PostgreSQL role that may create databases.');
	mkdirSync(stateDir, { recursive: true });

	const dbName = `wmt_e2e_${Date.now()}`;
	psql(adminUrl, `CREATE DATABASE ${dbName}`);
	const dbUrl = new URL(adminUrl);
	dbUrl.pathname = `/${dbName}`;

	const binary = resolve(stateDir, 'api');
	execFileSync('go', ['build', '-o', binary, './cmd/api'], { cwd: core, stdio: 'inherit' });

	const env = {
		...process.env,
		APP_ENV: 'local',
		API_ADDR: `127.0.0.1:${apiPort}`,
		DATABASE_URL: dbUrl.toString(),
		MIGRATION_DIR: resolve(core, 'migrations'),
		SESSION_SECRET: 'e2e-only-session-secret-0123456789abcdef',
		OTP_PEPPER: 'e2e-only-otp-pepper-0123456789abcdef',
		ALLOW_LOG_OTP: 'true',
		LOCAL_PAYMENT_SIMULATOR: 'true',
		PUBLIC_APP_ORIGIN: `http://127.0.0.1:${webPort}`,
		REDIS_URL: ''
	};
	execFileSync(binary, ['migrate'], { env, stdio: 'pipe' });

	const logPath = resolve(stateDir, 'api.log');
	const log = openSync(logPath, 'w');
	const api = spawn(binary, [], { env, stdio: ['ignore', log, log], detached: true });
	api.unref();
	await waitForHealth(`http://127.0.0.1:${apiPort}/health`, 30_000);

	const state: E2EState = { apiPid: api.pid!, adminUrl, dbName, logPath };
	writeFileSync(statePath, JSON.stringify(state));
}
