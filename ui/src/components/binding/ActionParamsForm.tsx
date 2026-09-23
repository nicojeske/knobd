import { useState } from "react";

import { ACTION_SPECS, type ActionType, type FieldSpec, type ParamsOf } from "../../actions/specs";
import { clearField, parseOptionalNumber, setField } from "../../config/fields";
import type { Target } from "../../types/config";
import styles from "./Field.module.css";
import { TargetField } from "./TargetField";

/** ActionParamsForm renders one action type's params entirely from its
 * ACTION_SPECS descriptor -- one generic renderer plus ~6 field
 * components, not 21 hand-written forms.
 *
 * TypeScript cannot correlate `field.kind` with the type of
 * `params[field.key]` across a distributed union like FieldSpec<P> --
 * narrowing `field` in the switch below doesn't narrow the indexed
 * access. The casts inside each case are confined to this file and each
 * is sound given the switch's own guard (the field descriptor's kind is
 * exactly what determines that key's value type in every ACTION_SPECS
 * entry -- see actions/specs.test.ts, which checks every descriptor's
 * fields against its own defaults()). */
export function ActionParamsForm<T extends ActionType>({
  type,
  params,
  onChange,
}: {
  type: T;
  params: ParamsOf<T>;
  onChange: (next: ParamsOf<T>) => void;
}) {
  const spec = ACTION_SPECS[type];

  if (spec.fields.length === 0) {
    return <p className={styles.unit}>This action takes no settings.</p>;
  }

  return (
    <div>
      {spec.fields.map((field) => (
        <FieldRow key={field.key} field={field} params={params} onChange={onChange} />
      ))}
    </div>
  );
}

function FieldRow<P extends object>({
  field,
  params,
  onChange,
}: {
  field: FieldSpec<P>;
  params: P;
  onChange: (next: P) => void;
}) {
  switch (field.kind) {
    case "number": {
      const raw = params[field.key];
      const value = typeof raw === "number" ? raw : undefined;
      return (
        <NumberFieldRow
          label={field.label}
          value={value}
          min={field.min}
          max={field.max}
          step={field.step}
          unit={field.unit}
          onCommit={(next) => {
            onChange(
              next === undefined
                ? clearField(params, field.key)
                : setField(params, field.key, next as P[typeof field.key]),
            );
          }}
        />
      );
    }
    case "text": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : "";
      return (
        <div className={styles.row}>
          <label>{field.label}</label>
          <input
            type="text"
            value={value}
            placeholder={field.placeholder}
            onChange={(e) => {
              const text = e.target.value;
              onChange(
                text === "" ? clearField(params, field.key) : setField(params, field.key, text as P[typeof field.key]),
              );
            }}
          />
        </div>
      );
    }
    case "enum": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : field.options[0];
      return (
        <div className={styles.row}>
          <label>{field.label}</label>
          <select
            value={value}
            onChange={(e) => {
              onChange(setField(params, field.key, e.target.value as P[typeof field.key]));
            }}
          >
            {field.options.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </div>
      );
    }
    case "target": {
      const value = params[field.key] as Target;
      return (
        <TargetField
          label={field.label}
          value={value}
          onChange={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
        />
      );
    }
    case "stringList": {
      const raw = params[field.key];
      const value = Array.isArray(raw) ? (raw as string[]) : [];
      return (
        <ListFieldRow
          label={field.label}
          value={value}
          placeholder="comma-separated"
          onCommit={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
          parse={(s) => s}
          format={(v) => v}
        />
      );
    }
    case "numberList": {
      const raw = params[field.key];
      const value = Array.isArray(raw) ? (raw as number[]) : [];
      return (
        <ListFieldRow
          label={field.label}
          value={value}
          placeholder="comma-separated numbers"
          onCommit={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
          parse={(s) => Number(s)}
          format={(v) => String(v)}
        />
      );
    }
  }
}

function NumberFieldRow({
  label,
  value,
  min,
  max,
  step,
  unit,
  onCommit,
}: {
  label: string;
  value: number | undefined;
  // Required-but-nullable, not `min?: number`: this component receives
  // its props from a spread of a generated FieldSpec, whose own optional
  // fields are `T | undefined` reads under exactOptionalPropertyTypes --
  // an `?:` prop here would reject that same value explicitly passed.
  min: number | undefined;
  max: number | undefined;
  step: number | undefined;
  unit: string | undefined;
  onCommit: (value: number | undefined) => void;
}) {
  const [text, setText] = useState(value === undefined ? "" : String(value));
  const [invalid, setInvalid] = useState(false);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      <div className={styles.inline}>
        <input
          type="text"
          inputMode="decimal"
          value={text}
          min={min}
          max={max}
          step={step}
          onChange={(e) => {
            setText(e.target.value);
          }}
          onBlur={() => {
            const parsed = parseOptionalNumber(text);
            if (!parsed.ok) {
              setInvalid(true);
              return;
            }
            setInvalid(false);
            onCommit(parsed.value);
          }}
        />
        {unit ? <span className={styles.unit}>{unit}</span> : null}
      </div>
      {invalid ? <div className={styles.error}>Not a number.</div> : null}
    </div>
  );
}

function ListFieldRow<Item>({
  label,
  value,
  placeholder,
  onCommit,
  parse,
  format,
}: {
  label: string;
  value: readonly Item[];
  placeholder: string;
  onCommit: (value: Item[]) => void;
  parse: (s: string) => Item;
  format: (v: Item) => string;
}) {
  const [text, setText] = useState(value.map(format).join(", "));

  return (
    <div className={styles.row}>
      <label>{label}</label>
      <input
        type="text"
        value={text}
        placeholder={placeholder}
        onChange={(e) => {
          setText(e.target.value);
        }}
        onBlur={() => {
          const items = text
            .split(",")
            .map((s) => s.trim())
            .filter((s) => s !== "")
            .map(parse);
          onCommit(items);
        }}
      />
    </div>
  );
}
