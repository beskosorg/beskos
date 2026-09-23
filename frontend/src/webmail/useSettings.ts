import { useQuery, useQueryClient } from "@tanstack/react-query";
import { settingsApi, type UserSettings } from "./backend";
import { toast } from "./toast";

// User preferences, shared between the Settings page (writes) and the apps (read).
// Everything goes through the same ["settings"] query, so a change in Settings is reflected right
// away in open apps (shared cache). The server merges by section; here we mirror that optimistically.

const KEY = ["settings"] as const;

export function useUserSettings(): UserSettings {
  const q = useQuery({ queryKey: KEY, queryFn: () => settingsApi.get(), staleTime: 60_000, retry: false });
  return q.data ?? {};
}

// useSection gives a Settings section its value plus a `save` that persists only that section
// (optimistic merge into the cache; persists to the server; reverts with a toast on error).
export function useSection<K extends keyof UserSettings>(app: K) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: KEY, queryFn: () => settingsApi.get(), staleTime: 60_000, retry: false });
  const value = (q.data?.[app] ?? {}) as NonNullable<UserSettings[K]>;

  const save = (patch: Partial<NonNullable<UserSettings[K]>>) => {
    const prev = qc.getQueryData<UserSettings>(KEY);
    const nextSection = { ...(prev?.[app] ?? {}), ...patch };
    qc.setQueryData<UserSettings>(KEY, { ...(prev ?? {}), [app]: nextSection });
    settingsApi.patch({ [app]: nextSection } as Partial<UserSettings>).catch((e) => {
      qc.setQueryData<UserSettings>(KEY, prev); // revert
      toast((e as Error).message);
    });
  };

  return { value, save, loading: q.isLoading };
}
