import { expect, test } from '@playwright/test';
import { marketingWording } from '../../src/lib/marketing-wording';
import { codeSentBy, newPerson, openEveryDay, setMode, signUpSeller } from './helpers';

test('a new seller gets a link suggested from their name and a public page', async ({ page }) => {
	const seller = newPerson('Ngozi', 'Adeyemi');
	await page.goto('/claim');
	await page.getByLabel('Your name').fill(seller.name);
	await expect(page.getByLabel('Your link')).toHaveValue(/^ngozi-?adeyemi\d?$/);

	const handle = await signUpSeller(page, seller);
	await expect(page.getByRole('heading', { name: /nearly open/i })).toBeVisible();

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

	await page.goto('/app/offers');
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

	await page.goto(`/${first}`);
	await expect(page).toHaveURL(new RegExp(`/${next}$`));
});
