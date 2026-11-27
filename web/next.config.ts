import type { NextConfig } from "next";

// Same default as lib/api/client.ts (DEFAULT_API_BASE_URL).
const DEFAULT_API_BASE_URL = "http://localhost:8080/api/v1";

/** Origin of the API, read from NEXT_PUBLIC_API_BASE_URL (build-time value). */
function apiOrigin(): string | null {
  const configured = process.env.NEXT_PUBLIC_API_BASE_URL;
  const base = configured !== undefined && configured !== "" ? configured : DEFAULT_API_BASE_URL;
  try {
    return new URL(base, "http://localhost").origin;
  } catch {
    return null;
  }
}

/**
 * Content-Security-Policy for production builds. Pragmatic, documented in
 * docs/deploy.md: Next.js emits inline bootstrap scripts and inline styles, so
 * `script-src` and `style-src` keep 'unsafe-inline' (a nonce would force every
 * page to render dynamically). 'unsafe-eval' is never allowed. PixiJS needs
 * blob: and data: images and blob: workers.
 */
function contentSecurityPolicy(): string {
  const api = apiOrigin();
  const connect = ["'self'", ...(api ? [api] : [])].join(" ");
  return [
    "default-src 'self'",
    "script-src 'self' 'unsafe-inline'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self' data:",
    `connect-src ${connect}`,
    "worker-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Development needs eval for hot reload, so headers apply to production only.
  async headers() {
    if (process.env.NODE_ENV !== "production") return [];
    return [
      {
        source: "/:path*",
        headers: [
          { key: "Content-Security-Policy", value: contentSecurityPolicy() },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "no-referrer" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()" },
          { key: "Strict-Transport-Security", value: "max-age=15552000; includeSubDomains" },
        ],
      },
    ];
  },
};

export default nextConfig;
