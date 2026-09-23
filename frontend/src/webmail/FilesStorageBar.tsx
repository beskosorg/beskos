// Storage quota indicator for the Drive top bar. Extracted from FilesView.tsx (law 1.4 -- 600 lines).
export function StorageBar({ used, max, label }: { used: number; max: number; fmt: (n: number) => string; label: string }) {
  const pct = max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0;
  return (
    <div className="storage-bar" title={label}>
      <div className="sb-track"><div className="sb-fill" style={{ width: `${pct}%` }} /></div>
      <span className="sb-label">{label}</span>
    </div>
  );
}
