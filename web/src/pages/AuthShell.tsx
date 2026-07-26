import type { FormEvent, ReactNode } from "react";

import { FilmIcon } from "../components/icons";
import { AlertStrip } from "../components/ui";

// The sign-in / sign-up surface: component-card chrome centred on the canvas,
// with the auth-page headline size. Login and Register share it so the two
// pages can never drift apart.
export function AuthShell({
  title,
  subtitle,
  error,
  onSubmit,
  children,
  footer,
}: {
  title: string;
  subtitle: string;
  error?: string | null;
  onSubmit: (e: FormEvent) => void;
  children: ReactNode;
  footer: ReactNode;
}) {
  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-[420px]">
        <div className="mb-6 flex items-center justify-center gap-2.5">
          <span className="flex size-10 items-center justify-center rounded-xl bg-brand-500 text-white">
            <FilmIcon className="size-6" />
          </span>
          <span className="text-theme-xl font-semibold text-gray-800 dark:text-white/90">
            TG Video
          </span>
        </div>

        <div className="card p-6 sm:p-8">
          <h1 className="text-title-md font-bold text-gray-900 dark:text-white/90">{title}</h1>
          <p className="mt-2 text-theme-sm text-gray-500 dark:text-gray-400">{subtitle}</p>

          <form onSubmit={onSubmit} className="mt-6 flex flex-col gap-4">
            {children}
          </form>

          {error && (
            <div className="mt-4">
              <AlertStrip title={error} />
            </div>
          )}

          <div className="mt-6 flex justify-center gap-1 text-theme-sm text-gray-500 dark:text-gray-400">
            {footer}
          </div>
        </div>
      </div>
    </div>
  );
}
