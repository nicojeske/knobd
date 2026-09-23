/** cx joins CSS Modules class names, dropping falsy values. Needed
 * everywhere a component combines more than one CSS-module class:
 * `vite/client`'s ambient `*.module.css` declaration types its default
 * export as `{ readonly [key: string]: string }`, and tsconfig's
 * noUncheckedIndexedAccess (see CLAUDE.md's TypeScript conventions)
 * applies to any access through an index signature -- dot notation
 * included -- so every `styles.foo` is `string | undefined`, not
 * `string`. A raw template literal (`` `${styles.a} ${styles.b}` ``)
 * both fails strict type-checking and would silently print "undefined"
 * if it didn't. */
export function cx(...classes: readonly (string | undefined | false)[]): string {
  return classes.filter((c): c is string => Boolean(c)).join(" ");
}
