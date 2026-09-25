// A seller's link is wantmytime.com/<handle>. These rules match the API's
// validHandle: 3 to 24 lowercase letters, numbers or single inner hyphens.
const reserved = new Set([
	'api',
	'metrics',
	'status',
	'healthz',
	'app',
	'ops',
	'admin',
	'login',
	'claim',
	'help',
	'pricing',
	'terms',
	'privacy',
	'booking',
	'offer',
	'checkout',
	'payment',
	'auth',
	'r',
	'og',
	'access',
	'health',
	'dev',
	'settings',
	'support',
	'www',
	'assets',
	// Top-level pages of the site itself.
	'book',
	'verify',
	'acceptable-use',
	'unsubscribe',
	'sitemap',
	'robots'
]);

export const handlePattern = '[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*';

export function validHandle(value: string): boolean {
	const h = value.trim().toLowerCase();
	return h.length >= 3 && h.length <= 24 && !reserved.has(h) && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(h);
}

/** Suggests a link from a display name: "Adá Obi" becomes "adaobi". */
export function suggestHandle(name: string): string {
	const plain = name.normalize('NFKD').replace(/[̀-ͯ]/g, '').toLowerCase();
	return plain.replace(/[^a-z0-9]+/g, '').slice(0, 24);
}

/** The link as people read it, without the scheme: wantmytime.com/adaobi. */
export function linkLabel(url: string): string {
	return url.replace(/^https?:\/\//, '').replace(/\/$/, '');
}
