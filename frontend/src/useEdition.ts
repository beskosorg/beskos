import { useQuery } from "@tanstack/react-query";
import { api, type Edition } from "./api";

// GET /api/edition needs no session (see api.ts): safe to call from public pages (Guest, Booking)
// as well as the authenticated app. Capabilities change only when an operator installs or renews
// a license, so a long staleTime avoids refetching on every navigation.
export function useEdition() {
  return useQuery<Edition>({ queryKey: ["edition"], queryFn: api.edition, staleTime: 5 * 60 * 1000 });
}

// branding() reads only the one capability most call sites need, defaulting to Community (false)
// while the query is still loading -- so a page never flashes the Pro-only UI and then hides it.
export function useBrandingCapability(): boolean {
  const { data } = useEdition();
  return data?.branding ?? false;
}
