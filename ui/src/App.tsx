import { useState } from "react";

import styles from "./App.module.css";
import { StatusBar } from "./components/common/StatusBar";
import { DiagnosticsView } from "./components/views/DiagnosticsView";
import { PlaceholderView } from "./components/views/PlaceholderView";
import { ConfigProvider } from "./state/ConfigContext";
import { ConnectionProvider } from "./state/ConnectionContext";

const VIEWS = ["panel", "apps", "profiles", "diagnostics"] as const;
type View = (typeof VIEWS)[number];

const VIEW_LABELS: Record<View, string> = {
  panel: "Panel",
  apps: "Apps & Groups",
  profiles: "Profiles",
  diagnostics: "Diagnostics",
};

function ViewContent({ view }: { view: View }) {
  switch (view) {
    case "panel":
      return (
        <PlaceholderView
          title="Panel"
          note="The visual X-Touch Mini panel and binding editor land in a later commit of this milestone."
        />
      );
    case "apps":
      return (
        <PlaceholderView
          title="Apps & Groups"
          note="App matcher and group management, backed by GET /audio, land in a later commit of this milestone."
        />
      );
    case "profiles":
      return <PlaceholderView title="Profiles" note="Profile management lands in a later commit of this milestone." />;
    case "diagnostics":
      return <DiagnosticsView />;
  }
}

export function App() {
  const [view, setView] = useState<View>("diagnostics");

  return (
    <ConnectionProvider>
      <ConfigProvider>
        <div className={styles.app}>
          <StatusBar />
          <nav className={styles.tabs}>
            {VIEWS.map((v) => (
              <button
                key={v}
                type="button"
                className={v === view ? `${styles.tab} ${styles.tabActive}` : styles.tab}
                onClick={() => setView(v)}
              >
                {VIEW_LABELS[v]}
              </button>
            ))}
          </nav>
          <div className={styles.content}>
            <ViewContent view={view} />
          </div>
        </div>
      </ConfigProvider>
    </ConnectionProvider>
  );
}
