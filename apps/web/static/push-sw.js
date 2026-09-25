// WantMyTime notifications. This worker only shows notifications the server
// sends and opens the page they point to; it caches nothing.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

self.addEventListener('push', (event) => {
	let data = {};
	try {
		data = event.data ? event.data.json() : {};
	} catch {
		data = { title: 'WantMyTime', body: event.data ? event.data.text() : '' };
	}
	event.waitUntil(
		self.registration.showNotification(data.title || 'WantMyTime', {
			body: data.body || '',
			icon: '/icon-192.png',
			badge: '/badge-96.png',
			tag: data.tag || undefined,
			renotify: Boolean(data.tag),
			data: { url: data.url || '/' }
		})
	);
});

self.addEventListener('notificationclick', (event) => {
	event.notification.close();
	let url = '/';
	try {
		const target = new URL((event.notification.data && event.notification.data.url) || '/', self.location.origin);
		if (target.origin === self.location.origin) url = target.href;
	} catch {
		url = '/';
	}
	event.waitUntil(
		(async () => {
			const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
			for (const client of open) {
				if (client.url === url && 'focus' in client) return client.focus();
			}
			return self.clients.openWindow(url);
		})()
	);
});

// The browser replaced the subscription: register the new one.
self.addEventListener('pushsubscriptionchange', (event) => {
	event.waitUntil(
		(async () => {
			const config = await (await fetch('/api/v1/push/config', { credentials: 'include' })).json();
			if (!config.enabled) return;
			const key = Uint8Array.from(atob(config.public_key.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
			const sub = await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
			await fetch('/api/v1/push/subscriptions', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(sub.toJSON()) });
		})()
	);
});
