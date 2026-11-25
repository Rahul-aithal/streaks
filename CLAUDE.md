# Working in this repo

## UI: shadcn first

Every piece of web UI (`apps/web`) comes from shadcn/ui before anything else.

1. Look for a shadcn component for the job: button, input, calendar and popover
   (date picker), collapsible, dialog, dropdown, tooltip, tabs, and so on.
2. If it isn't in `apps/web/src/components/ui` yet, add it with
   `pnpm --filter web exec shadcn add <name>`. Don't hand-write a copy.
3. Compose the shadcn parts into a feature component in `src/components`
   (see `date-picker.tsx`). Only build from scratch when shadcn has nothing close.

Don't use the browser's native widgets where shadcn has one. For example, no
`<input type="date">`, `<details>` or bare `<button>` styled by hand.

Anything a user can press must look like a button: use `Button` with a variant
(`secondary` or `outline` still reads as a button when it's disabled; `ghost` doesn't).

## Speed

- Start a page's requests in its route `loader` (`void queryClient.prefetchQuery(...)`)
  so they run with the navigation, not after the component renders. Shared query
  definitions live in `src/lib/queries.ts`.
- Lazy-load anything heavy or not seen on first paint with `lazy()`. Hover or focus
  may warm only the cheap parts (a page's code and its request, the small rewards
  panel, the habit form). The heavy ones (the calendar, the heatmap) load when they
  are opened, behind a skeleton, never on hover.
- Keep borders and shadows on the tokens in `styles.css` (`border-border`,
  `shadow-card`, `border-control` for empty check circles), not ad-hoc colours.

## Checks

`make test` and `make lint` must pass. The Worker and `lib/query-retry.ts` have a
100% coverage gate.
