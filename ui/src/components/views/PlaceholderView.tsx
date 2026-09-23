import styles from "./DiagnosticsView.module.css";

/** PlaceholderView stands in for a view whose real content lands in a
 * later commit of this milestone (see specs/milestones/M07-config-ui.md) —
 * the panel, the app/group pickers, and profile management. */
export function PlaceholderView({ title, note }: { title: string; note: string }) {
  return (
    <div className={styles.view}>
      <section className={styles.section}>
        <h2>{title}</h2>
        <p>{note}</p>
      </section>
    </div>
  );
}
