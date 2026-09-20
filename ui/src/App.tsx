// Placeholder root component. The real config UI (visual X-Touch Mini
// panel, MIDI learn, profile/binding management) is specced in
// specs/milestones/M07-config-ui.md and not built yet — this exists so
// the Vite/Tauri toolchain has something to render while that milestone
// is in progress.
export function App() {
  return (
    <main>
      <h1>knobd</h1>
      <p>
        Configuration UI scaffolding — see{" "}
        <code>specs/milestones/M07-config-ui.md</code> for what gets built
        here.
      </p>
    </main>
  );
}
