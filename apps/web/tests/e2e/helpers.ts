import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { expect, type Page } from '@playwright/test';
import { statePath, type E2EState } from './state';

function state(): E2EState {
	return JSON.parse(readFileSync(statePath, 'utf8'));
}

let seq = 0;
/** A name, email and expected link that no other test run uses. */
export function newPerson(first: string, last: string) {
	const tag = `${Date.now().toString(36)}${(seq++).toString(36)}`;
	return { name: `${first} ${last}`, email: `${first.toLowerCase()}.${tag}@e2e.test`, tag };
}

function codeLines(email: string): string[] {
	// The API logs local sign-in codes against the first 12 hex digits of sha256(email).
	const hash = createHash('sha256').update(email.trim().toLowerCase()).digest('hex').slice(0, 12);
	return readFileSync(state().logPath, 'utf8')
		.split('\n')
		.filter((line) => line.includes(`LOCAL OTP email_hash=${hash} `));
}

/** Runs an action that sends a sign-in code to email, and returns that code. */
export async function codeSentBy(email: string, action: () => Promise<unknown>): Promise<string> {
	const before = codeLines(email).length;
	await action();
	let lines: string[] = [];
	await expect.poll(() => (lines = codeLines(email)).length, { timeout: 10_000 }).toBeGreaterThan(before);
	return lines.at(-1)!.match(/code=(\d+)/)![1];
}

/** Signs up through /claim and lands in the workspace. Returns the link it got. */
export async function signUpSeller(
	page: Page,
	person: { name: string; email: string },
	handle?: string,
	options: { news?: boolean } = {}
) {
	await page.goto('/claim');
	await page.getByLabel('Your name').fill(person.name);
	const link = page.getByLabel('Your link');
	if (handle) await link.fill(handle);
	await expect(page.locator('#claim-handle-note')).toContainText('is yours if you want it');
	const chosen = await link.inputValue();
	await page.getByLabel('Your email').fill(person.email);
	if (options.news === false) await page.getByLabel(/Send me news/).uncheck();
	const code = await codeSentBy(person.email, () => page.getByRole('button', { name: /Send my code/ }).click());
	await expect(page).toHaveURL(/\/verify/);
	// The page submits on its own once all eight digits are in.
	await page.getByLabel('Your code').fill(code);
	await expect(page).toHaveURL(/\/app$/);
	return chosen;
}

/** Opens weekly hours (every day, 06:00 to 22:00) through the API, as the signed-in seller. */
export async function openEveryDay(page: Page) {
	const windows = [0, 1, 2, 3, 4, 5, 6].map((weekday) => ({ weekday, start: '06:00', end: '22:00' }));
	const response = await page.request.put('/api/v1/me/availability', {
		headers: { origin: new URL(page.url()).origin },
		data: {
			timezone: 'Africa/Lagos',
			minimum_notice_minutes: 60,
			booking_horizon_days: 30,
			buffer_minutes: 0,
			windows,
			overrides: []
		}
	});
	expect(response.ok()).toBeTruthy();
}

/** Saves a link mode through the API, as the signed-in seller. */
export async function setMode(page: Page, mode: 'fixed' | 'both') {
	const headers = { origin: new URL(page.url()).origin };
	const link = await (await page.request.get('/api/v1/me/link')).json();
	const response = await page.request.patch('/api/v1/me/link', { headers, data: { ...link, mode } });
	expect(response.ok()).toBeTruthy();
}
