import type { HandleClientError } from '@sveltejs/kit';
import { env } from '$env/dynamic/public';
import { reportError } from '$lib/observe/sentry';

export const handleError: HandleClientError = ({ error, event, status, message }) => {
	const reference = crypto.randomUUID().replaceAll('-', '');
	if (status >= 500) {
		reportError(error, {
			dsn: env.PUBLIC_SENTRY_DSN,
			platform: 'javascript',
			environment: env.PUBLIC_APP_ENV,
			release: env.PUBLIC_APP_RELEASE,
			tags: { route: event.route.id ?? 'unmatched', status: String(status), reference }
		});
	}
	return { message: status >= 500 ? 'Something went wrong on this page.' : message, reference };
};
