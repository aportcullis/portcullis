import type { PermissionKey } from "@/entities/session/store";

// A page is reachable when any capability it offers is held. Navigation is an affordance; the server authorizes each RPC (ADR-0008).
export type Section = { href: string; label: string; capabilities: PermissionKey[] };

// The authenticated sections in nav order. A section's capabilities are the permissions its page can do something useful with.
export const sections: readonly Section[] = [
  // Registering a connection stands on its own permission, so create alone opens this page — the same union the requests section uses. The list inside it is still gated on connections.list.
  { href: "/connections", label: "Connections", capabilities: ["connections.list", "connections.create"] },
  { href: "/requests", label: "Requests", capabilities: ["requests.list", "requests.create"] },
];

// PermissionCheck answers "does the signed-in principal hold this key?" — the session store's lookup, passed in so these stay pure and testable.
export type PermissionCheck = (permission: PermissionKey) => boolean;

// visibleSections returns the sections the caller can open at all.
export function visibleSections(hasPermission: PermissionCheck): Section[] {
  return sections.filter((s) => s.capabilities.some(hasPermission));
}

// landingFor picks where "/" goes: the first section the caller can actually open. Falling back to /requests when nothing is open keeps the old behaviour for a permissionless account — the page itself explains the emptiness rather than the router bouncing between denied routes.
export function landingFor(hasPermission: PermissionCheck): string {
  return visibleSections(hasPermission)[0]?.href ?? "/requests";
}
