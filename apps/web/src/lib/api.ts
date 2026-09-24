// Browser traffic stays same-origin so HttpOnly sessions work through the
// reverse proxy. Server-side loads call the API themselves with
// `$env/dynamic/private` API_INTERNAL_BASE; this helper is for browser code.
const API = import.meta.env.VITE_API_BASE ?? '';

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) }
  });
  let body: unknown = null;
  if (response.status !== 204) {
    const text = await response.text();
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        // A gateway error page (for example a 502 from Nginx) is not JSON.
        if (!response.ok) throw new Error('The service is temporarily unavailable. Please try again.');
        throw new Error('The service returned an unexpected response. Please try again.');
      }
    }
  }
  if (!response.ok) {
    const message = (body as { error?: { message?: string } } | null)?.error?.message ?? 'Something went wrong. Please try again.';
    // Server errors carry a reference that matches the API logs and error reports.
    const reference = response.status >= 500 ? response.headers.get('x-request-id') : null;
    throw new Error(reference && !message.includes(reference) ? `${message} (Reference: ${reference})` : message);
  }
  return body as T;
}

export { API };
