export const SESSION_TYPE_FILTER_KEY = "ui.filter.sessionType";

const defaultStorage = (): Storage | null => {
  try {
    return sessionStorage;
  } catch {
    return null;
  }
};

export function readTypeFilter(storage: Storage | null = defaultStorage()): string | null {
  if (!storage) return null;

  try {
    return storage.getItem(SESSION_TYPE_FILTER_KEY);
  } catch {
    return null;
  }
}

export function writeTypeFilter(value: string | null, storage: Storage | null = defaultStorage()): void {
  if (!storage) return;

  try {
    if (value === null) {
      storage.removeItem(SESSION_TYPE_FILTER_KEY);
      return;
    }
    storage.setItem(SESSION_TYPE_FILTER_KEY, value);
  } catch {
    return;
  }
}
