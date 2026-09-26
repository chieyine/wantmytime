import { env } from '$env/dynamic/public';
import { brand } from '$lib/brand';

/**
 * The public address of the site, for links people share and see:
 * https://wantmytime.com. In the browser it is the address the person is on.
 * On the server it comes from PUBLIC_APP_ORIGIN; if that was set to the API's
 * address (api.wantmytime.com), the api. is dropped, since a personal link on
 * the API host would not open the booking page.
 */
export function siteOrigin(): string {
	if (typeof window !== 'undefined') return window.location.origin;
	return configuredSiteOrigin();
}

export function configuredSiteOrigin(): string {
	const fallback = `https://${brand.domain}`;
	try {
		const url = new URL((env.PUBLIC_APP_ORIGIN || fallback).trim());
		const host = url.host.toLowerCase().replace(/^api\./, '');
		return `${url.protocol}//${host}`;
	} catch {
		return fallback;
	}
}
