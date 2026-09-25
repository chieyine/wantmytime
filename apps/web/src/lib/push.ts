import { api } from '$lib/api';

// Self-hosted web push: the browser subscribes with WantMyTime's public key,
// and the server sends encrypted notifications straight to the browser's own
// push service. Nothing here talks to a third party.

export type PushState = 'unsupported' | 'install-first' | 'blocked' | 'off' | 'on' | 'unavailable';

type Config = { enabled: boolean; public_key?: string };

function isIOS(): boolean {
	return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
}

function isStandalone(): boolean {
	return window.matchMedia('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true;
}

function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
	const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
	const raw = atob(base64 + '='.repeat((4 - (base64.length % 4)) % 4));
	const out = new Uint8Array(new ArrayBuffer(raw.length));
	for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
	return out;
}

let configPromise: Promise<Config> | null = null;
const config = () => (configPromise ??= api<Config>('/api/v1/push/config').catch(() => ({ enabled: false })));

async function registration(): Promise<ServiceWorkerRegistration> {
	return navigator.serviceWorker.register('/push-sw.js', { scope: '/' });
}

export async function pushState(): Promise<PushState> {
	if (typeof window === 'undefined') return 'unsupported';
	const supported = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
	if (!supported) return isIOS() && !isStandalone() ? 'install-first' : 'unsupported';
	if (!(await config()).enabled) return 'unavailable';
	if (Notification.permission === 'denied') return 'blocked';
	const existing = await navigator.serviceWorker.getRegistration('/');
	const sub = existing ? await existing.pushManager.getSubscription() : null;
	return sub && Notification.permission === 'granted' ? 'on' : 'off';
}

export async function enablePush(): Promise<PushState> {
	const cfg = await config();
	if (!cfg.enabled || !cfg.public_key) return 'unavailable';
	const permission = await Notification.requestPermission();
	if (permission !== 'granted') return permission === 'denied' ? 'blocked' : 'off';
	const reg = await registration();
	await navigator.serviceWorker.ready;
	const sub = (await reg.pushManager.getSubscription()) ?? (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(cfg.public_key) }));
	await api('/api/v1/push/subscriptions', { method: 'POST', body: JSON.stringify(sub.toJSON()) });
	return 'on';
}

export async function disablePush(): Promise<PushState> {
	const reg = await navigator.serviceWorker.getRegistration('/');
	const sub = reg ? await reg.pushManager.getSubscription() : null;
	if (sub) {
		await api('/api/v1/push/subscriptions', { method: 'DELETE', body: JSON.stringify({ endpoint: sub.endpoint }) }).catch(() => undefined);
		await sub.unsubscribe();
	}
	return 'off';
}

// A browser already switched on is re-linked to whoever is signed in now,
// so a buyer who later becomes a seller keeps getting their notifications.
export async function refreshPush(): Promise<void> {
	if ((await pushState()) !== 'on') return;
	const reg = await navigator.serviceWorker.getRegistration('/');
	const sub = reg ? await reg.pushManager.getSubscription() : null;
	if (sub) await api('/api/v1/push/subscriptions', { method: 'POST', body: JSON.stringify(sub.toJSON()) }).catch(() => undefined);
}
