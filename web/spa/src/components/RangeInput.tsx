// ============================================================
// RangeInput — reusable "min ~ max" dual-number widget
// Phase 2-4 range fields will use this component.
// ============================================================

interface RangeInputProps {
  min: number;
  max: number;
  onChangeMin: (v: number) => void;
  onChangeMax: (v: number) => void;
  disabled?: boolean;
  step?: number;
  minLabel?: string;
  maxLabel?: string;
}

export function RangeInput({
  min,
  max,
  onChangeMin,
  onChangeMax,
  disabled,
  step = 1,
  minLabel = '最小',
  maxLabel = '最大',
}: RangeInputProps) {
  return (
    <div className="flex items-center gap-2">
      <div className="flex flex-col flex-1 min-w-0">
        <span className="text-xs text-muted mb-0.5">{minLabel}</span>
        <input
          type="number"
          value={min}
          onChange={(e) => onChangeMin(Number(e.target.value))}
          disabled={disabled}
          step={step}
          className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition
                     disabled:cursor-not-allowed"
        />
      </div>
      <span className="text-muted text-sm mt-4">~</span>
      <div className="flex flex-col flex-1 min-w-0">
        <span className="text-xs text-muted mb-0.5">{maxLabel}</span>
        <input
          type="number"
          value={max}
          onChange={(e) => onChangeMax(Number(e.target.value))}
          disabled={disabled}
          step={step}
          className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition
                     disabled:cursor-not-allowed"
        />
      </div>
    </div>
  );
}
