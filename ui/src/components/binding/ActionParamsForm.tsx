import { useState } from "react";

import { ACTION_SPECS, type ActionType, type FieldSpec, type ParamsOf } from "../../actions/specs";
import { clearField, parseOptionalNumber, setField } from "../../config/fields";
import { useConfig } from "../../state/ConfigContext";
import { useConnection } from "../../state/ConnectionContext";
import { useSpotifyDevices, useSpotifyPlaylists } from "../../state/useSpotify";
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
    case "scene": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : "";
      return (
        <SceneFieldRow
          label={field.label}
          value={value}
          onChange={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
        />
      );
    }
    case "player": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : "";
      return (
        <PlayerFieldRow
          label={field.label}
          value={value}
          onChange={(next) => {
            onChange(
              next === "" ? clearField(params, field.key) : setField(params, field.key, next as P[typeof field.key]),
            );
          }}
        />
      );
    }
    case "playlist": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : "";
      return (
        <PlaylistFieldRow
          label={field.label}
          value={value}
          editableOnly={field.editableOnly ?? false}
          onChange={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
        />
      );
    }
    case "device": {
      const raw = params[field.key];
      const value = typeof raw === "string" ? raw : "";
      return (
        <DeviceFieldRow
          label={field.label}
          value={value}
          onChange={(next) => {
            onChange(setField(params, field.key, next as P[typeof field.key]));
          }}
        />
      );
    }
    case "boolean": {
      const raw = params[field.key];
      const value = raw === true;
      return (
        <div className={styles.row}>
          <label>{field.label}</label>
          <input
            type="checkbox"
            checked={value}
            onChange={(e) => {
              onChange(
                e.target.checked
                  ? setField(params, field.key, true as P[typeof field.key])
                  : clearField(params, field.key),
              );
            }}
          />
        </div>
      );
    }
  }
}

/** PlayerFieldRow picks an MPRIS player by ref (see media.PlayerInfo.
 * Ref) from the live GET /state player list -- an empty value means
 * "whichever player is currently selected" (PlayerRef's own empty-string
 * meaning, see model.MediaTransportAction). A saved ref that matches no
 * currently-running player (the app isn't open right now) still shows as
 * its own option, the same "known option, or type it yourself" shape
 * SceneFieldRow uses for a deleted scene. */
function PlayerFieldRow({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
}) {
  const { state } = useConnection();
  const players = state?.media.players ?? [];
  const knownOption = value === "" || players.some((p) => p.ref === value);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      {knownOption ? (
        <select
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        >
          <option value="">Selected player</option>
          {players.map((p) => (
            <option key={p.ref} value={p.ref}>
              {p.identity ?? p.ref}
              {p.ignored ? " (ignored)" : ""}
            </option>
          ))}
        </select>
      ) : (
        <input
          type="text"
          value={value}
          placeholder="player ref"
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      )}
    </div>
  );
}

/** PlaylistFieldRow picks a Spotify playlist by id from
 * GET /spotify/playlists (empty until connected -- see
 * useSpotifyPlaylists's own doc comment), falling back to free text
 * (accepting a playlist id, a spotify:playlist:... uri, or an
 * open.spotify.com/playlist/... link -- the daemon normalizes any of
 * those, see daemon/internal/spotify.ParseID) the same "known option, or
 * type it yourself" shape TargetField/SceneFieldRow use. editableOnly
 * narrows the picker to playlists the authorized user can add/remove
 * items from (add_to_playlist/remove_from_playlist); start_playlist
 * leaves it false, since starting playback works on any playlist. */
function PlaylistFieldRow({
  label,
  value,
  editableOnly,
  onChange,
}: {
  label: string;
  value: string;
  editableOnly: boolean;
  onChange: (next: string) => void;
}) {
  const { playlists, loading, error } = useSpotifyPlaylists();
  const options = (playlists ?? []).filter((p) => !editableOnly || p.editable);
  const knownOption = value === "" || options.some((p) => p.id === value);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      {knownOption && playlists !== undefined ? (
        <select
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        >
          <option value="" disabled>
            {options.length === 0 ? "(no playlists available)" : "Choose a playlist…"}
          </option>
          {options.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name || p.id}
            </option>
          ))}
        </select>
      ) : (
        <input
          type="text"
          value={value}
          placeholder="playlist id, uri, or open.spotify.com link"
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      )}
      {loading ? <span className={styles.unit}>Loading…</span> : null}
      {error ? <div className={styles.error}>{error}</div> : null}
    </div>
  );
}

/** DeviceFieldRow picks a Spotify Connect device by name from
 * GET /spotify/devices -- model.SpotifyTransferPlaybackAction.DeviceName
 * matches by name (case-insensitively), not id, since device ids aren't
 * stable across client restarts (see that field's own doc comment). */
function DeviceFieldRow({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
}) {
  const { devices, loading, error } = useSpotifyDevices();
  const knownOption = value === "" || (devices ?? []).some((d) => d.name === value);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      {knownOption && devices !== undefined ? (
        <select
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        >
          <option value="" disabled>
            {devices.length === 0 ? "(no devices available)" : "Choose a device…"}
          </option>
          {devices.map((d) => (
            <option key={d.id} value={d.name}>
              {d.name}
              {d.active ? " (active)" : ""}
            </option>
          ))}
        </select>
      ) : (
        <input
          type="text"
          value={value}
          placeholder="device name"
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      )}
      {loading ? <span className={styles.unit}>Loading…</span> : null}
      {error ? <div className={styles.error}>{error}</div> : null}
    </div>
  );
}

/** SceneFieldRow picks a scene by id from config.scenes -- falling back
 * to free text if the stored sceneId doesn't match any configured scene
 * (a scene deleted out from under an existing binding, or a hand-edited
 * config), the same "known option, or type it yourself" shape
 * TargetField uses for a device that isn't plugged in right now. */
function SceneFieldRow({ label, value, onChange }: { label: string; value: string; onChange: (next: string) => void }) {
  const { config } = useConfig();
  const scenes = config?.scenes ?? [];
  const knownOption = value === "" || scenes.some((s) => s.id === value);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      {knownOption ? (
        <select
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        >
          <option value="" disabled>
            {scenes.length === 0 ? "(no scenes configured)" : "Choose a scene…"}
          </option>
          {scenes.map((s) => (
            <option key={s.id} value={s.id}>
              {s.displayName || s.id}
            </option>
          ))}
        </select>
      ) : (
        <input
          type="text"
          value={value}
          placeholder="scene id"
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      )}
    </div>
  );
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
