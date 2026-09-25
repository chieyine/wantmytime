import { error, redirect } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { env } from '$env/dynamic/private';
import { formatMoney } from '$lib/money';

// Sanitized, fixed-size public vector preview. A future approved raster renderer
// can emit PNG while preserving the same data and URL contract.
export const GET: RequestHandler = async ({ params, fetch }) => {
	const handle = params.handle.toLowerCase();
	if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(handle) || !/^\d+$/.test(params.version)) {
		throw error(404, 'Preview unavailable.');
	}
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	let person;
	try {
		const response = await fetch(`${apiBase}/api/v1/people/${encodeURIComponent(handle)}`);
		if (!response.ok) throw error(404, 'Preview unavailable.');
		person = await response.json();
	} catch (cause) {
		if (cause && typeof cause === 'object' && 'status' in cause) throw cause;
		throw error(503, 'Preview temporarily unavailable.');
	}
	const current = Math.max(1, Number(person.public_version || 1));
	if (Number(params.version) !== current) throw redirect(302, `/og/${encodeURIComponent(handle)}/${current}.svg`);
	const esc = (value: string) => value.replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]!);
	const name = esc(person.name);
	const publicHandle = esc(person.handle);	const siteHost = esc(new URL(env.PUBLIC_APP_ORIGIN || 'https://wantmytime.com').host);
	const price = person.mode !== 'offer' && !person.paused
		? formatMoney(Math.round(Number(person.base_30_minor) / 100) * 100, person.currency || 'NGN')
		: '';
	const detail = price ? `${esc(price)} / 30 minutes` : 'BOOK TIME ON YOUR TERMS';
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630"><rect width="1200" height="630" fill="#f2f0e9"/><path d="M72 82H1128M72 546H1128" stroke="#171817" stroke-width="2"/><text x="72" y="122" font-family="Arial,sans-serif" font-size="22" font-weight="700" letter-spacing="3" fill="#171817">WANTMYTIME® / PERSONAL BOOKING</text><text x="72" y="280" font-family="Arial,sans-serif" font-size="82" font-weight="800" letter-spacing="-5" fill="#171817">A CONVERSATION</text><text x="72" y="370" font-family="Baskerville,Georgia,serif" font-size="96" fill="#171817">with ${name}</text><text x="72" y="462" font-family="Arial,sans-serif" font-size="34" fill="#171817">${detail}</text><text x="72" y="585" font-family="Arial,sans-serif" font-size="23" letter-spacing="2" fill="#555a53">${siteHost}/${publicHandle}</text><circle cx="1035" cy="442" r="28" fill="none" stroke="#b43a30" stroke-width="8"/><line x1="1035" y1="442" x2="1035" y2="425" stroke="#171817" stroke-width="3"/><line x1="1035" y1="442" x2="1049" y2="449" stroke="#171817" stroke-width="3"/></svg>`;
	return new Response(svg, {
		headers: {
			'Content-Type': 'image/svg+xml; charset=utf-8',
			'Cache-Control': 'public, max-age=86400, stale-while-revalidate=604800',
			'ETag': `"${handle}-${params.version}-svg"`,
			'X-Content-Type-Options': 'nosniff'
		}
	});
};
