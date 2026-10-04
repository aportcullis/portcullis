/** The element that had focus before a panel opened, reduced to what returning focus needs. */
export type FocusReturnTarget = { isConnected: boolean; focus: (options?: FocusOptions) => void };

/** Returns focus to the opener when the closing panel held it; reports whether focus moved. */
export function returnFocusToOpener(opener: FocusReturnTarget | undefined, panelHeldFocus: boolean): boolean {
  if (!opener || !panelHeldFocus || !opener.isConnected) return false;
  opener.focus({ preventScroll: true });
  return true;
}
