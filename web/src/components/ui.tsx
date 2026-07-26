import type { ReactNode } from "react";

// Shared surfaces built from the design tokens in tailwind.config.js. Pages
// compose these instead of restating chrome (and never hard-code a hex).

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

// PageHeader is the standard page opener: sentence-case title on the left,
// actions on the right, with an optional count/meta line underneath.
export function PageHeader({
  title,
  meta,
  actions,
}: {
  title: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-5 flex flex-wrap items-center gap-3">
      <div className="min-w-0">
        <h1 className="truncate text-theme-xl font-semibold text-gray-800 dark:text-white/90">
          {title}
        </h1>
        {meta && (
          <div className="mt-0.5 text-theme-xs text-gray-500 dark:text-gray-400">{meta}</div>
        )}
      </div>
      {actions && <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

// Card is the universal container: white surface, hairline border, 16px
// radius, flat. `padded` is the common case; pass false for edge-to-edge lists.
export function Card({
  children,
  className,
  padded = true,
}: {
  children: ReactNode;
  className?: string;
  padded?: boolean;
}) {
  return <div className={cx("card", padded && "p-5", className)}>{children}</div>;
}

// FilterBar is the surface that holds a page's search/sort controls, sitting
// above the content grid.
export function FilterBar({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={cx("card p-4", className)}>{children}</div>;
}

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cx(
        "inline-block animate-spin rounded-full border-2 border-gray-200 border-t-brand-500 dark:border-gray-700 dark:border-t-brand-400",
        className ?? "size-4",
      )}
      aria-hidden="true"
    />
  );
}

export function LoadingState({ label = "加载中…" }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2.5 py-16 text-theme-sm text-gray-500 dark:text-gray-400">
      <Spinner className="size-4" />
      {label}
    </div>
  );
}

// EmptyState frames a "nothing here" message in card chrome with a centered
// muted caption and an optional next action.
export function EmptyState({
  title,
  hint,
  action,
}: {
  title: ReactNode;
  hint?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="card flex flex-col items-center gap-2 px-6 py-10 text-center">
      <div className="text-base text-gray-700 dark:text-gray-300">{title}</div>
      {hint && <div className="max-w-md text-theme-sm text-gray-500 dark:text-gray-400">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

// AlertStrip — surface card with a semantic left accent. Used for inline
// notices and form/query failures.
const ALERT_TONE = {
  error: "border-l-error-500 text-error-600 dark:text-error-400",
  warning: "border-l-warning-500 text-warning-600 dark:text-warning-400",
  info: "border-l-blue-light-500 text-blue-light-600 dark:text-blue-light-400",
  success: "border-l-success-500 text-success-600 dark:text-success-400",
} as const;

export function AlertStrip({
  tone = "error",
  title,
  children,
}: {
  tone?: keyof typeof ALERT_TONE;
  title: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className={cx("card border-l-4 p-4 shadow-theme-sm", ALERT_TONE[tone])}>
      <div className="text-theme-sm font-medium">{title}</div>
      {children && (
        <div className="mt-1 text-theme-xs text-gray-500 dark:text-gray-400">{children}</div>
      )}
    </div>
  );
}

// MoreFooter is the shared "load more / all loaded" footer under an infinite
// list. Takes the three fields it needs rather than a query object, so it works
// with every page's own Page type. The empty case belongs to VideoGrid.
export function MoreFooter({
  hasNextPage,
  isFetchingNextPage,
  fetchNextPage,
  doneLabel,
  loaded,
  pageSize,
}: {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => void;
  doneLabel: string;
  loaded: number;
  pageSize: number;
}) {
  if (loaded === 0) return null;
  return (
    <div className="flex items-center justify-center pb-2 pt-1">
      {hasNextPage ? (
        <button onClick={fetchNextPage} disabled={isFetchingNextPage} className="btn btn-outline">
          {isFetchingNextPage ? (
            <>
              <Spinner className="size-4" />
              加载中…
            </>
          ) : (
            `加载下一页 (+${pageSize})`
          )}
        </button>
      ) : (
        <span className="text-theme-xs text-gray-400 dark:text-gray-500">
          — {doneLabel} {loaded.toLocaleString()} 条 —
        </span>
      )}
    </div>
  );
}

// Toggle — the brand-filled switch used for per-channel booleans.
export function Toggle({
  checked,
  onChange,
  disabled,
  label,
  title,
  size = "md",
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  label?: ReactNode;
  title?: string;
  size?: "sm" | "md";
}) {
  const track = size === "sm" ? "h-5 w-9" : "h-6 w-11";
  const knob = size === "sm" ? "size-4" : "size-5";
  const shift = size === "sm" ? "translate-x-4" : "translate-x-5";
  return (
    <label
      className="flex select-none items-center gap-2 text-theme-sm text-gray-600 dark:text-gray-400"
      title={title}
    >
      {label}
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={typeof label === "string" ? label : undefined}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cx(
          "relative shrink-0 rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50",
          track,
          checked ? "bg-brand-500" : "bg-gray-200 dark:bg-gray-700",
        )}
      >
        <span
          className={cx(
            "absolute left-0.5 top-0.5 rounded-full bg-white shadow-theme-xs transition-transform",
            knob,
            checked && shift,
          )}
        />
      </button>
    </label>
  );
}
