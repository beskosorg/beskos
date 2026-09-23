// iOS-style toggle switch (on/off). Accessible: it's a <button role=switch> with aria-checked, so
// it works via keyboard and screen reader — unlike a painted <div>.
export function Switch({ checked, onChange, disabled, label }: {
  checked: boolean;
  onChange: (on: boolean) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} disabled={disabled}
      className={"switch" + (checked ? " on" : "")} onClick={() => onChange(!checked)}>
      <span className="knob" />
    </button>
  );
}
