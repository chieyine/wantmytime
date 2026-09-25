import { env } from '$env/dynamic/public';
import { brand } from '$lib/brand';
import type { RequestHandler } from './$types';

// Only the public site. Sellers' pages stay out of search on purpose (they are
// marked noindex), so people choose who finds them.
const paths = ['/', '/pricing', '/help', '/terms', '/privacy', '/acceptable-use'];

export const GET: RequestHandler = () => {
	const origin = (env.PUBLIC_APP_ORIGIN || `https://${brand.domain}`).replace(/\/$/, '');
	const urls = paths.map((path) => `  <url><loc>${origin}${path}</loc></url>`).join('\n');
	const body = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`;
	return new Response(body, { headers: { 'content-type': 'application/xml; charset=utf-8', 'cache-control': 'public, max-age=3600' } });
};
