import { expect, test } from '@playwright/test';
import { marketingWording } from '../../src/lib/marketing-wording';
import { codeSentBy, newPerson, openEveryDay, setMode, signUpSeller } from './helpers';

test('a new seller gets a link suggested from their name and a public page', async ({ page }) => {
	const seller = newPerson('Ngozi', 'Adeyemi');
	await page.goto('/claim');
	await page.getByLabel('Your name').fill(seller.name);
	await expect(page.getByLabel('Your link')).toHaveValue(/^ngozi-?adeyemi\d?$/);

	const handle = await signUpSeller(page, seller);
	await expect(page.getByRole('heading', { name: /few steps from your first booking/i })).toBeVisible();

	// The pre-ticked news box reached the API, with the same wording people saw.
	const news = await (await page.request.get('/api/v1/me/marketing')).json();
	expect(news).toMatchObject({ subscribed: true, confirmed: true, wording: marketingWording });

	await page.goto(`/${handle}`);
	await expect(page.getByRole('heading', { level: 1, name: seller.name })).toBeVisible();
});

test('a buyer books and pays, and the seller sees the booking', async ({ page, browser }) => {
	const seller = newPerson('Tobi', 'Lawal');
	const handle = await signUpSeller(page, seller, `tobi-${seller.tag}`);
	await openEveryDay(page);

	const buyerContext = await browser.newContext();
	const buyer = await buyerContext.newPage();
	await buyer.goto(`/${handle}`);
	await buyer.getByRole('link', { name: /pick a time/i }).click();
	await expect(buyer).toHaveURL(/\/book\/new/);
	await buyer.getByRole('group', { name: 'Available times' }).getByRole('button').first().click();
	await expect(buyer.getByLabel(/Send me news/)).toBeChecked();
	await buyer.getByLabel(/Send me news/).uncheck();
	await buyer.getByLabel('Your name').fill('Kemi Ade');
	await buyer.getByLabel('Email', { exact: true }).fill(`kemi.${seller.tag}@e2e.test`);
	await buyer.getByRole('button', { name: /continue to payment/i }).click();
	await expect(buyer).toHaveURL(/\/checkout\//);
	await buyer.getByRole('button', { name: /confirm test booking/i }).click();
	await expect(buyer).toHaveURL(/\/booking\//);
	await expect(buyer.getByText(/confirmed/i).first()).toBeVisible();
	await buyerContext.close();

	await page.goto('/app/bookings');
	await expect(page.getByText('Kemi Ade').first()).toBeVisible();
});

test('a seller taking both a price and offers accepts an offer', async ({ page, browser }) => {
	const seller = newPerson('Musa', 'Bello');
	const handle = await signUpSeller(page, seller, `musa-${seller.tag}`);
	await openEveryDay(page);
	await setMode(page, 'both');

	const buyerContext = await browser.newContext();
	const buyer = await buyerContext.newPage();
	await buyer.goto(`/${handle}`);
	await expect(buyer.getByRole('link', { name: /pick a time/i })).toBeVisible();
	await buyer.getByRole('link', { name: /make an offer/i }).click();
	await expect(buyer).toHaveURL(/\/offer\/new/);
	await buyer.getByLabel(/Offer in/).fill('8000');
	await buyer.getByLabel('Your name').fill('Ifeoma Nnaji');
	const buyerEmail = `ifeoma.${seller.tag}@e2e.test`;
	await buyer.getByLabel('Your email').fill(buyerEmail);
	const code = await codeSentBy(buyerEmail, () => buyer.getByRole('button', { name: /send offer/i }).click());
	await expect(buyer).toHaveURL(/\/verify/);
	await buyer.getByLabel('Your code').fill(code);
	await expect(buyer).toHaveURL(/\/offer\/[0-9a-f-]{36}/);
	await buyerContext.close();

	await page.goto('/app/bookings');
	await page.getByText('Ifeoma Nnaji').first().click();
	await page.getByRole('button', { name: /^Accept/ }).click();
	await expect(page.getByText(/Accepted\. Ifeoma Nnaji has been emailed/)).toBeVisible();
});

test('changing the link keeps the old one working', async ({ page }) => {
	const seller = newPerson('Efe', 'Okoro');
	const first = await signUpSeller(page, seller, `efe-${seller.tag}`);
	const next = `efe-new-${seller.tag}`;

	await page.goto('/app/link');
	await page.getByLabel('Your link').fill(next);
	await expect(page.locator('#link-handle-note')).toContainText('is free');
	await page.getByRole('button', { name: 'Change link' }).click();
	await expect(page.locator('#link-handle-note')).toContainText(`/${next}`);
	// Once every six months: the field locks and says when it opens again.
	await page.reload();
	await expect(page.locator('#link-handle-note')).toContainText('You can change it again on');
	await expect(page.getByLabel('Your link')).toHaveAttribute('readonly', '');

	await page.goto(`/${first}`);
	await expect(page).toHaveURL(new RegExp(`/${next}$`));
});

test('a taken link offers free ones to pick, like Gmail', async ({ page, browser }) => {
	const first = newPerson('Kelechi', 'Nwosu');
	const taken = `kelechi-${first.tag}`;
	await signUpSeller(page, first, taken);

	const other = await (await browser.newContext()).newPage();
	await other.goto('/claim');
	await other.getByLabel('Your name').fill(first.name);
	await expect(other.getByRole('group', { name: 'Available links' }).getByRole('button').first()).toBeVisible();
	await other.getByLabel('Your link').fill(taken);
	await expect(other.locator('#claim-handle-note')).toContainText('That link is taken');
	const choice = other.getByRole('group', { name: 'Available links' }).getByRole('button').first();
	const picked = await choice.innerText();
	await choice.click();
	await expect(other.getByLabel('Your link')).toHaveValue(picked);
	await expect(other.locator('#claim-handle-note')).toContainText('is yours if you want it');
	await other.context().close();
});

test('a seller sets their hours by ticking days', async ({ page }) => {
	const seller = newPerson('Halima', 'Garba');
	await signUpSeller(page, seller);
	await page.goto('/app/availability');
	// A first visit starts from weekdays, 9 to 5.
	await expect(page.getByRole('checkbox', { name: 'Monday' })).toBeChecked();
	await expect(page.getByRole('checkbox', { name: 'Sunday' })).not.toBeChecked();

	await page.getByLabel('Monday until').fill('13:00');
	await page.getByRole('button', { name: 'Copy to every open day' }).click();
	await page.getByRole('checkbox', { name: 'Friday' }).uncheck();
	await page.getByRole('checkbox', { name: 'Saturday' }).check();
	await page.getByRole('button', { name: /save hours/i }).click();
	await expect(page.getByText('HOURS SAVED')).toBeVisible();

	const saved = await (await page.request.get('/api/v1/me/availability')).json();
	const days = saved.windows.map((w: { weekday: number; end: string }) => `${w.weekday}-${w.end.slice(0, 5)}`);
	expect(days).toEqual(['1-13:00', '2-13:00', '3-13:00', '4-13:00', '6-13:00']);
});

test('a seller who skipped news at sign-up is asked once on the dashboard', async ({ page }) => {
	const seller = newPerson('Chidi', 'Eze');
	await signUpSeller(page, seller, undefined, { news: false });
	await page.goto('/app');
	const prompt = page.getByRole('region', { name: 'Hear about new features first.' });
	await expect(prompt).toBeVisible();
	await expect(prompt).toContainText(marketingWording);
	await prompt.getByRole('button', { name: 'Yes, send me news' }).click();
	await expect(page.getByText('You’re in.')).toBeVisible();
	expect(await (await page.request.get('/api/v1/me/marketing')).json()).toMatchObject({ subscribed: true, ask: false });

	await page.reload();
	await page.waitForLoadState('networkidle');
	await expect(page.locator('.news-prompt')).toHaveCount(0);
});
