import { expect, test } from '@playwright/test';
import { newPerson, openEveryDay, setMode, signUpSeller } from './helpers';

// Guards the responsive work: no page may scroll sideways, on phone or desktop.
const publicPages = ['/', '/pricing', '/claim', '/login', '/help', '/terms', '/privacy'];
const workspacePages = [
	'/app',
	'/app/bookings',
	'/app/offers',
	'/app/link',
	'/app/availability',
	'/app/money',
	'/app/share',
	'/app/settings'
];

async function sidewaysOverflow(page: import('@playwright/test').Page) {
	return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
}

test('public pages fit the screen', async ({ page }) => {
	for (const path of publicPages) {
		await page.goto(path);
		expect(await sidewaysOverflow(page), path).toBeLessThanOrEqual(0);
	}
});

test('workspace and seller pages fit the screen', async ({ page }) => {
	const seller = newPerson('Zainab', 'Sule');
	const handle = await signUpSeller(page, seller, `zainab-${seller.tag}`);
	await openEveryDay(page);
	await setMode(page, 'both');
	for (const path of [...workspacePages, `/${handle}`]) {
		await page.goto(path);
		await page.waitForLoadState('networkidle');
		expect(await sidewaysOverflow(page), path).toBeLessThanOrEqual(0);
	}
});
